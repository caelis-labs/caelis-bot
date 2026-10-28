package sshtransport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// Authentication is transient transport input, never a Worker approval/receipt.
// No password, key bytes, raw stderr or config content is written to logs.
type tunnel struct {
	target                api.SSHTarget
	state, issue, address string
	cancel                context.CancelFunc
	done                  chan struct{}
	prompt                *api.SSHPrompt
	answer                chan string
	token                 string
	retryAt               time.Time
	failures              int
	stopped               bool
	released              bool
	generation            uint64
	pid                   int
}

func (t *Transport) Status(v api.SSHTarget) api.SSHConnectionStatus {
	key, e := targetKey(v)
	if e != nil {
		return api.SSHConnectionStatus{State: "failed", Issue: e.Error()}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.status(t.tunnels[key])
}
func (t *Transport) status(c *tunnel) api.SSHConnectionStatus {
	if c == nil {
		return api.SSHConnectionStatus{State: "idle"}
	}
	if c.stopped {
		issue := "ssh_stopping"
		select {
		case <-c.done:
			issue = "ssh_stopped"
		default:
		}
		return api.SSHConnectionStatus{State: "failed", Issue: issue}
	}
	v := api.SSHConnectionStatus{State: c.state, Issue: c.issue}
	if c.prompt != nil {
		p := *c.prompt
		v.Prompt = &p
	}
	return v
}

// Start is explicit, returns immediately and never waits for a person. Existing
// live channels are reused. Stopping channels retain the target until cleanup
// completes; only a subsequent explicit Start clears a user-action boundary.
func (t *Transport) Start(v api.SSHTarget) (api.SSHConnectionStatus, error) {
	key, e := targetKey(v)
	if e != nil {
		return api.SSHConnectionStatus{}, e
	}
	if v.Port == 0 {
		v.Port = 9317
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return api.SSHConnectionStatus{}, errors.New("ssh_closed")
	}
	if runtime.GOOS == "windows" {
		return api.SSHConnectionStatus{}, errors.New("ssh_platform_unsupported")
	}
	if t.binary == "" {
		return api.SSHConnectionStatus{}, errors.New("ssh_not_installed")
	}
	if len(t.tunnels) >= 16 && t.tunnels[key] == nil {
		// ReleaseUnused owns reclamation. Forgetting a stopped/blocked entry here
		// would let a later health read silently recreate it.
		return api.SSHConnectionStatus{}, errors.New("ssh_channel_limit")
	}
	old := t.tunnels[key]
	if old != nil {
		select {
		case <-old.done:
		default:
			if old.stopped {
				return t.status(old), errors.New("ssh_stopping")
			}
			return t.status(old), nil
		}
	}
	c := t.startLocked(key, v, 0, true)
	return t.status(c), nil
}
func (t *Transport) startLocked(key string, v api.SSHTarget, failures int, interactive bool) *tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	t.generation++
	c := &tunnel{target: v, state: "connecting", cancel: cancel, done: make(chan struct{}), failures: failures, generation: t.generation}
	t.tunnels[key] = c
	t.diagnoseLocked(c, "starting", "", 0, nil)
	go t.run(ctx, c, interactive)
	return c
}
func (t *Transport) Resolve(ctx context.Context, address string) (string, error) {
	if strings.HasPrefix(address, "ssh://") {
		if runtime.GOOS == "windows" {
			return "", errors.New("ssh_platform_unsupported")
		}
		if t.binary == "" {
			return "", errors.New("ssh_not_installed")
		}
	}
	v, ok := Target(address)
	if !ok {
		if strings.HasPrefix(address, "ssh:") {
			return "", errors.New("ssh_target_invalid")
		}
		return address, nil
	}
	key, _ := targetKey(v)
	t.mu.Lock()
	c := t.tunnels[key]
	if t.closed {
		t.mu.Unlock()
		return "", errors.New("ssh_closed")
	}
	if c == nil {
		if len(t.tunnels) >= 16 {
			t.mu.Unlock()
			return "", errors.New("ssh_channel_limit")
		}
		c = t.startLocked(key, v, 0, false)
	} else if c.state == "failed" && !c.stopped && retryable(c.issue) && !t.now().Before(c.retryAt) {
		c = t.startLocked(key, v, c.failures, false)
	}
	if c.stopped {
		t.mu.Unlock()
		return "", errors.New("ssh_stopped")
	}
	if c.state == "ready" {
		a := c.address
		t.mu.Unlock()
		return a, nil
	}
	done := c.done
	state := c.state
	t.mu.Unlock()
	if state == "authentication" {
		return "", errors.New("ssh_authentication_required")
	}
	if state == "failed" {
		return "", errors.New("ssh_reconnect_required")
	}
	// No shared lock waits on a network, authentication or another peer.
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-wait.Done():
			return "", errors.New("ssh_connecting")
		case <-done:
			return "", errors.New("ssh_reconnect_required")
		case <-tick.C:
			t.mu.Lock()
			state, addr, stopped := c.state, c.address, c.stopped
			t.mu.Unlock()
			if stopped {
				return "", errors.New("ssh_stopped")
			}
			if state == "ready" {
				return addr, nil
			}
			if state == "authentication" {
				return "", errors.New("ssh_authentication_required")
			}
		}
	}
}
func (t *Transport) Answer(in api.SSHAnswer) error {
	target := api.SSHTarget{Host: in.Host, Port: in.Port}
	if in.Target != nil {
		target = *in.Target
	}
	key, e := targetKey(target)
	if e != nil {
		return e
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.tunnels[key]
	if c == nil || c.prompt == nil || c.prompt.ID != in.ID {
		return errors.New("ssh_prompt_expired")
	}
	if in.Cancel {
		c.stopped = true
		c.cancel()
		return nil
	}
	if len(in.Value) > 4096 || strings.ContainsAny(in.Value, "\r\n\x00") {
		return errors.New("ssh_answer_invalid")
	}
	if c.prompt.Confirm && in.Value != "yes" && in.Value != "no" {
		return errors.New("ssh_answer_invalid")
	}
	select {
	case c.answer <- in.Value:
		if c.prompt.Confirm && in.Value == "no" {
			c.stopped = true
		}
		c.prompt = nil
		return nil
	default:
		return errors.New("ssh_prompt_expired")
	}
}
func (t *Transport) Stop(v api.SSHTarget) error {
	key, e := targetKey(v)
	if e != nil {
		return e
	}
	t.mu.Lock()
	c := t.tunnels[key]
	if c != nil {
		c.stopped = true
		c.cancel()
	}
	t.mu.Unlock()
	if c != nil {
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			return errors.New("ssh_stopping")
		}
	}
	return nil
}

// Release only channels the owning connection service has proved unreferenced.
// Keep authentication/connecting episodes alive; never infer task ownership here.
// Cancellation is nonblocking. Keep the target indexed until cleanup completes,
// so Start cannot overlap its process and Close still joins it.
func (t *Transport) ReleaseUnused(addresses []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, address := range addresses {
		target, ok := Target(address)
		if !ok {
			continue
		}
		key, err := targetKey(target)
		if err != nil {
			continue
		}
		c := t.tunnels[key]
		if c == nil || (c.state != "ready" && c.state != "failed") {
			continue
		}
		c.stopped = true
		c.released = true
		c.cancel()
		select {
		case <-c.done:
			delete(t.tunnels, key)
		default:
			// run removes this same generation after all owned resources exit.
		}
	}
}

// FailedChannelAddresses lets the connection owner prove which completed attempts no
// longer have live/profile references before admitting another explicit target.
func (t *Transport) FailedChannelAddresses() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	addresses := make([]string, 0, len(t.tunnels))
	for key, c := range t.tunnels {
		if c.state == "failed" {
			addresses = append(addresses, key)
		}
	}
	return addresses
}
func (t *Transport) Close() {
	t.mu.Lock()
	t.closed = true
	wait := make([]chan struct{}, 0, len(t.tunnels))
	for _, c := range t.tunnels {
		c.stopped = true
		c.cancel()
		wait = append(wait, c.done)
	}
	t.mu.Unlock()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for _, done := range wait {
		select {
		case <-done:
		case <-deadline.C:
			return
		}
	}
}
func (t *Transport) run(ctx context.Context, c *tunnel, interactive bool) {
	issue := "ssh_connection_failed"
	var exitCode *int
	defer func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		phase := "blocked"
		var delay time.Duration
		if c.stopped || issue == "ssh_stopped" {
			c.stopped, issue, phase = true, "ssh_stopped", "stopped"
		} else if retryable(issue) {
			// Saturation only protects the counter; it never ends recovery.
			c.failures = min(c.failures+1, 32)
			delay = retryDelay(c.failures)
			c.retryAt, phase = t.now().Add(delay), "retry_wait"
		}
		c.state = "failed"
		c.issue = issue
		c.prompt = nil
		c.token = ""
		t.diagnoseLocked(c, phase, issue, delay, exitCode)
		// Publish terminal state only after the child, broker and private socket
		// have all exited. A concurrent Resolve cannot overlap generations.
		close(c.done)
		if c.released {
			key, _ := targetKey(c.target)
			if t.tunnels[key] == c {
				delete(t.tunnels, key)
			}
		}
	}()
	if e := localstate.EnsurePrivateDirectory(t.root); e != nil {
		issue = "ssh_config_unreadable"
		return
	}
	dir, e := os.MkdirTemp("", "caelis-ssh-")
	if e != nil {
		return
	}
	defer os.RemoveAll(dir)
	config := "none"
	if c.target.Authentication == "" {
		t.configMu.Lock()
		config, e = t.compose(dir, false, "")
		t.configMu.Unlock()
	}
	if e != nil {
		issue = "ssh_config_unreadable"
		if e.Error() == "ssh_system_include_dynamic" {
			issue = e.Error()
		}
		return
	}
	select {
	case <-ctx.Done():
		issue = "ssh_stopped"
		return
	default:
	}
	control := filepath.Join(dir, "control")
	args := []string{"-N", "-T", "-F", config, "-M", "-S", control, "-o", "ControlPersist=no", "-o", "ExitOnForwardFailure=yes", "-o", "ConnectTimeout=10", "-o", "ConnectionAttempts=1", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2"}
	manual, err := t.manualArguments(c.target)
	if err != nil {
		issue = err.Error()
		return
	}
	args = append(args, manual...)
	if !interactive {
		args = append(args, "-o", "BatchMode=yes")
	}
	cmd := exec.CommandContext(ctx, t.binary, append(args, c.target.Host)...)
	cmd.Stdout = io.Discard
	var diag diagnostic
	cmd.Stderr = &diag
	bound := bindProcess(cmd)
	var broker *http.Server
	if interactive {
		socket, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			return
		}
		token := rand.Text() + rand.Text()
		t.mu.Lock()
		c.token = token
		t.mu.Unlock()
		broker = &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, MaxHeaderBytes: 4096, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/ask" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Origin") != "" {
				http.Error(w, "denied", 403)
				return
			}
			var input struct {
				Text string `json:"text"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&input) != nil || len(input.Text) > 4096 {
				http.Error(w, "invalid", 400)
				return
			}
			// OpenSSH's exact host-key fingerprint/identity remains visible to the user.
			confirm := strings.Contains(input.Text, "(yes/no") || strings.Contains(input.Text, "yes/no/")
			answer := make(chan string, 1)
			id := rand.Text()
			t.mu.Lock()
			if c.prompt != nil {
				t.mu.Unlock()
				http.Error(w, "busy", 409)
				return
			}
			c.prompt = &api.SSHPrompt{ID: id, Text: input.Text, Confirm: confirm}
			c.answer = answer
			c.state = "authentication"
			t.mu.Unlock()
			defer func() {
				t.mu.Lock()
				if c.prompt != nil && c.prompt.ID == id {
					c.prompt = nil
				}
				t.mu.Unlock()
			}()
			select {
			case value := <-answer:
				t.mu.Lock()
				c.state = "connecting"
				t.mu.Unlock()
				_, _ = io.WriteString(w, value)
			case <-ctx.Done():
				http.Error(w, "cancelled", 409)
			case <-r.Context().Done():
			case <-time.After(2 * time.Minute):
				http.Error(w, "expired", 408)
				c.cancel()
			}
		})}
		go func() { _ = broker.Serve(socket) }()
		defer broker.Close()
		// Main executable handles askpass before starting App services. No helper
		// script copies config/passwords and no API receipt persists secret arguments.
		cmd.Env = append(os.Environ(), "SSH_ASKPASS="+t.helper, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=caelis-ssh", "CAELIS_SSH_ASKPASS="+socket.Addr().String(), "CAELIS_SSH_ASKPASS_TOKEN="+token)
	}
	if e = cmd.Start(); e != nil {
		issue = "ssh_not_installed"
		return
	}
	t.mu.Lock()
	c.pid = cmd.Process.Pid
	t.diagnoseLocked(c, "started", "", 0, nil)
	t.mu.Unlock()
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() {
		c.cancel()
		bound()
		<-exited
		code := cmd.ProcessState.ExitCode()
		exitCode = &code
	}()
	// Start with all inherited forwards disabled. Only a second command, with
	// -F none and our private control socket, adds our single loopback forward.
	// Unlike ClearAllForwardings=yes plus -L on one command, this does not erase
	// the requested forward or inherit arbitrary config forwards.
	deadline := time.NewTimer(2 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			issue = "ssh_stopped"
			return
		case <-exited:
			issue = diag.code()
			return
		case <-deadline.C:
			issue = "ssh_authentication_timeout"
			return
		case <-tick.C:
			checkCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			check := exec.CommandContext(checkCtx, t.binary, "-F", "none", "-S", control, "-O", "check", c.target.Host)
			err := check.Run()
			cancel()
			if err != nil {
				continue
			}
			// OS selects an idle port. The owned master's forward command is the only
			// readiness proof; an unrelated TCP listener can never certify this channel.
			var port int
			for range 3 {
				l, e := net.Listen("tcp4", "127.0.0.1:0")
				if e != nil {
					break
				}
				port = l.Addr().(*net.TCPAddr).Port
				l.Close()
				fctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				f := exec.CommandContext(fctx, t.binary, "-F", "none", "-S", control, "-O", "forward", "-o", "ExitOnForwardFailure=yes", "-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", port, c.target.Port), c.target.Host)
				e = f.Run()
				cancel()
				if e == nil {
					break
				}
				port = 0
			}
			if port == 0 {
				issue = "ssh_forward_failed"
				return
			}
			t.mu.Lock()
			c.state = "ready"
			c.issue = ""
			c.address = fmt.Sprintf("http://127.0.0.1:%d", port)
			t.diagnoseLocked(c, "ready", "", 0, nil)
			c.failures = 0
			c.retryAt = time.Time{}
			_ = diag.code() // Discard startup warnings before classifying a later exit.
			t.mu.Unlock()
			select {
			case <-ctx.Done():
				issue = "ssh_stopped"
				return
			case <-exited:
				issue = diag.code()
				if issue == "ssh_authentication_or_connection_failed" {
					// This generation already authenticated and owned a forward.
					issue = "ssh_disconnected"
				}
				return
			}
		}
	}

}

// Askpass is entered only by a child ssh process with a private transient broker.
// The token never appears in command-line arguments, durable state or logs.
func RunAskpass(args []string) (bool, error) {
	endpoint := os.Getenv("CAELIS_SSH_ASKPASS")
	if endpoint == "" {
		return false, nil
	}
	host, _, e := net.SplitHostPort(endpoint)
	if e != nil || host != "127.0.0.1" || len(args) != 2 {
		return true, errors.New("ssh_askpass_invalid")
	}
	b, _ := json.Marshal(map[string]string{"text": args[1]})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", "http://"+endpoint+"/ask", strings.NewReader(string(b)))
	if e != nil {
		return true, e
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("CAELIS_SSH_ASKPASS_TOKEN"))
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("denied") }}
	res, e := client.Do(req)
	if e != nil {
		return true, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return true, errors.New("ssh_askpass_cancelled")
	}
	_, e = io.Copy(os.Stdout, io.LimitReader(res.Body, 4096))
	return true, e
}
