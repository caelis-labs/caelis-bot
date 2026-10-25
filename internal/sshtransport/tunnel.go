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
	"strconv"
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
	lastAttempt           time.Time
	attempts              int
}

func targetKey(v api.SSHTarget) (string, error) {
	if v.Port == 0 {
		v.Port = 9317
	}
	if !validHost(v.Host) || v.Port < 1 || v.Port > 65535 {
		return "", errors.New("ssh_target_invalid")
	}
	return fmt.Sprintf("%s:%d", v.Host, v.Port), nil
}
func Target(address string) (api.SSHTarget, bool) {
	if !strings.HasPrefix(address, "ssh://") {
		return api.SSHTarget{}, false
	}
	host, port, e := net.SplitHostPort(strings.TrimPrefix(address, "ssh://"))
	n, e2 := strconv.Atoi(port)
	v := api.SSHTarget{Host: host, Port: n}
	_, e3 := targetKey(v)
	return v, e == nil && e2 == nil && e3 == nil
}
func Address(v api.SSHTarget) (string, error) {
	if v.Port == 0 {
		v.Port = 9317
	}
	if _, e := targetKey(v); e != nil {
		return "", e
	}
	return "ssh://" + net.JoinHostPort(v.Host, strconv.Itoa(v.Port)), nil
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
	v := api.SSHConnectionStatus{State: c.state, Issue: c.issue}
	if c.prompt != nil {
		p := *c.prompt
		v.Prompt = &p
	}
	return v
}

// Start is explicit, returns immediately and never waits for a person. Existing
// live channels are reused. Automatic reads have a separate finite retry budget.
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
		for key, c := range t.tunnels {
			if c.state == "failed" {
				select {
				case <-c.done:
					delete(t.tunnels, key)
				default:
				}
			}
		}
		if len(t.tunnels) >= 16 {
			return api.SSHConnectionStatus{}, errors.New("ssh_channel_limit")
		}
	}
	old := t.tunnels[key]
	if old != nil && (old.state == "connecting" || old.state == "authentication" || old.state == "ready") {
		return t.status(old), nil
	}
	c := t.startLocked(key, v, 0, true)
	return t.status(c), nil
}
func (t *Transport) startLocked(key string, v api.SSHTarget, attempts int, interactive bool) *tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	c := &tunnel{target: v, state: "connecting", cancel: cancel, done: make(chan struct{}), lastAttempt: time.Now(), attempts: attempts}
	t.tunnels[key] = c
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
		c = t.startLocked(key, v, 1, false)
	} else if c.state == "failed" && c.attempts < 3 && time.Since(c.lastAttempt) >= time.Duration(1<<c.attempts)*5*time.Second {
		c = t.startLocked(key, v, c.attempts+1, false)
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
			state, addr := c.state, c.address
			t.mu.Unlock()
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
	key, e := targetKey(api.SSHTarget{Host: in.Host, Port: in.Port})
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
		c.attempts = 3
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
func (t *Transport) Close() {
	t.mu.Lock()
	t.closed = true
	var wait []chan struct{}
	for _, c := range t.tunnels {
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
	defer func() {
		t.mu.Lock()
		c.state = "failed"
		c.issue = issue
		c.prompt = nil
		c.token = ""
		t.mu.Unlock()
		close(c.done)
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
	t.configMu.Lock()
	config, e := t.compose(dir, false, "")
	t.configMu.Unlock()
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
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() { c.cancel(); bound(); <-exited }()
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
			t.mu.Unlock()
			select {
			case <-ctx.Done():
				issue = "ssh_stopped"
				return
			case <-exited:
				issue = "ssh_disconnected"
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
