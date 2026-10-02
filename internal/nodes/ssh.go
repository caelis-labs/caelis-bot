package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// SSHConfig is native private configuration. It is never tool/renderer input.
type SSHConfig struct {
	Protocol                             caelis.WorkerProtocol
	Target, Helper, Store, WorkspaceRoot string
	Binary                               string
	HelperArgs                           []string
}
type SSHWorker struct {
	config   SSHConfig
	mu       sync.Mutex
	expected caelis.WorkerBootstrapResult
	tunnel   *SSHTunnel
	closed   bool
}

var sshTarget = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@:-]*$`)

func NewSSHWorker(config SSHConfig) (*SSHWorker, error) {
	if config.Protocol != "" && config.Protocol != caelis.WorkerProtocolSharedNative && config.Protocol != caelis.WorkerProtocolBoundedApplication {
		return nil, errors.New("invalid Worker protocol")
	}

	if !sshTarget.MatchString(config.Target) {
		return nil, errors.New("invalid SSH target")
	}
	if config.Helper == "" {
		config.Helper = "caelis-worker-bootstrap"
	}
	if strings.ContainsAny(config.Helper, "\x00\r\n") {
		return nil, errors.New("invalid SSH helper")
	}
	if config.Binary == "" {
		config.Binary = "ssh"
	}
	return &SSHWorker{config: config}, nil
}
func (s *SSHWorker) args() []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UpdateHostKeys=no", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "ForwardX11Trusted=no", "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ForkAfterAuthentication=no", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=15"}
}
func sshQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
func (s *SSHWorker) helper(ctx context.Context, req caelis.WorkerBootstrapRequest) (caelis.WorkerBootstrapResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var result caelis.WorkerBootstrapResult
	body, err := json.Marshal(req)
	if err != nil {
		return result, err
	}
	command := sshQuote(s.config.Helper)
	for _, arg := range s.config.HelperArgs {
		command += " " + sshQuote(arg)
	}
	args := append(s.args(), "--", s.config.Target, command)
	cmd := exec.CommandContext(ctx, s.config.Binary, args...)
	cmd.Stdin = bytes.NewReader(body)
	// Bound stdout; never return stderr or command text (may contain private
	// config/proxy diagnostics). Credentials are stdin bytes, never argv/env.
	var output limitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil || output.overflow {
		return result, errors.New("SSH Worker bootstrap unavailable")
	}
	if json.Unmarshal(output.Bytes(), &result) != nil {
		return result, errors.New("SSH Worker bootstrap response incompatible")
	}
	return result, nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

// Probe is read-only: no tunnel, enrollment, workspace or remote installation.
func (s *SSHWorker) Probe(ctx context.Context) (caelis.WorkerBootstrapResult, error) {
	return s.helper(ctx, caelis.WorkerBootstrapRequest{Action: "probe", Store: s.config.Store, Protocol: s.config.Protocol})
}
func (s *SSHWorker) Endpoint(ctx context.Context) (caelis.WorkerEndpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return caelis.WorkerEndpoint{}, errors.New("SSH Worker detached")
	}
	ready, err := s.Probe(ctx)
	if err != nil {
		return caelis.WorkerEndpoint{}, err
	}
	if ready.StoreID == "" || ready.InstanceID == "" || ready.PrincipalID == "" {
		return caelis.WorkerEndpoint{}, errors.New("SSH Worker identity unavailable")
	}
	if s.expected.StoreID != "" && (s.expected.StoreID != ready.StoreID || s.expected.PrincipalID != ready.PrincipalID) {
		return caelis.WorkerEndpoint{}, errors.New("SSH Worker identity changed")
	}
	if s.tunnel != nil {
		s.tunnel.Close()
	}
	tunnel, err := s.openTunnel(ready.Endpoint)
	if err != nil {
		return caelis.WorkerEndpoint{}, err
	}
	s.expected = ready
	s.tunnel = tunnel
	// This enrollment belongs to this probe, even if another reconnect changes
	// the current Host instance before the closure is used.
	enrollment := s.request("enroll")
	return caelis.WorkerEndpoint{Capabilities: ready.Capabilities, Origin: tunnel.Origin(), StoreID: ready.StoreID, InstanceID: ready.InstanceID, PrincipalID: ready.PrincipalID, Execution: ready.Execution, ModelConfigured: ready.ModelConfigured, ModelAuth: ready.ModelAuth, Enroll: func(ctx context.Context, op, secret string) (wire.ApplicationConnection, error) {
		req := enrollment
		req.OperationID = op
		req.AppCredential = secret
		out, err := s.helper(ctx, req)
		if err != nil {
			return wire.ApplicationConnection{}, err
		}
		if !sameSSHIdentity(ready, out) || out.Connection == nil {
			return wire.ApplicationConnection{}, errors.New("SSH Worker enrollment identity mismatch")
		}
		return *out.Connection, nil
	}}, nil
}
func sameSSHIdentity(a, b caelis.WorkerBootstrapResult) bool {
	return a.StoreID == b.StoreID && a.InstanceID == b.InstanceID && a.PrincipalID == b.PrincipalID
}
func (s *SSHWorker) request(action string) caelis.WorkerBootstrapRequest {
	// Caller either owns mu (Endpoint) or copies under mu in workspace methods.
	return caelis.WorkerBootstrapRequest{Action: action, Store: s.config.Store, Protocol: s.config.Protocol, WorkspaceRoot: s.config.WorkspaceRoot, StoreID: s.expected.StoreID, InstanceID: s.expected.InstanceID, PrincipalID: s.expected.PrincipalID}
}
func (s *SSHWorker) ResolveWorkWorkspace(ctx context.Context, id, requested string) (string, error) {
	s.mu.Lock()
	req := s.request("resolve_workspace")
	s.mu.Unlock()
	req.TaskID = id
	req.Workspace = requested
	req.Selected = requested != ""
	out, err := s.helper(ctx, req)
	return out.Workspace, err
}
func (s *SSHWorker) PrepareWorkWorkspace(ctx context.Context, id, workspace string, selected bool) error {
	s.mu.Lock()
	req := s.request("prepare_workspace")
	s.mu.Unlock()
	req.TaskID = id
	req.Workspace = workspace
	req.Selected = selected
	out, err := s.helper(ctx, req)
	if err == nil && out.Workspace != workspace {
		return errors.New("SSH Worker workspace changed")
	}
	return err
}

// Detach closes only owned observation/forwarding; no Host shutdown or cancel.
func (s *SSHWorker) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.tunnel != nil {
		return s.tunnel.Close()
	}
	return nil
}

// SSHTunnel owns a local loopback listener. Each HTTP connection uses standard
// SSH direct-tcpip (-W), preventing SSH config forwarding from opening ports.
type SSHTunnel struct {
	listener    net.Listener
	cancel      context.CancelFunc
	mu          sync.Mutex
	connections map[net.Conn]bool
	wg          sync.WaitGroup
}

func (t *SSHTunnel) Origin() string { return "http://" + t.listener.Addr().String() }
func (s *SSHWorker) openTunnel(endpoint string) (*SSHTunnel, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("SSH Worker endpoint incompatible")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return nil, errors.New("SSH Worker requires a loopback Host endpoint")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("SSH Worker tunnel unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &SSHTunnel{listener: ln, cancel: cancel, connections: map[net.Conn]bool{}}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.mu.Lock()
			if ctx.Err() != nil {
				t.mu.Unlock()
				conn.Close()
				return
			}
			t.connections[conn] = true
			t.wg.Add(1)
			t.mu.Unlock()
			go func() {
				defer t.wg.Done()
				defer conn.Close()
				defer func() { t.mu.Lock(); delete(t.connections, conn); t.mu.Unlock() }()
				args := append(s.args(), "-W", net.JoinHostPort(u.Hostname(), u.Port()), "--", s.config.Target)
				cmd := exec.CommandContext(ctx, s.config.Binary, args...)
				cmd.Stderr = io.Discard
				stdin, err := cmd.StdinPipe()
				if err != nil {
					return
				}
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					return
				}
				if cmd.Start() != nil {
					return
				}
				copied := make(chan struct{})
				go func() { io.Copy(stdin, conn); stdin.Close(); close(copied) }()
				io.Copy(conn, stdout)
				conn.Close()
				stdin.Close()
				cmd.Wait()
				<-copied
			}()
		}
	}()
	return t, nil
}
func (t *SSHTunnel) Close() error {
	t.cancel()
	err := t.listener.Close()
	t.mu.Lock()
	for conn := range t.connections {
		conn.Close()
	}
	t.mu.Unlock()
	t.wg.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
