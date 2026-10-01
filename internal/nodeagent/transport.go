package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localipc"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func allowed(method, path string) bool {
	return method == "GET" && path == "/v1/node/catalog" || method == "POST" && (path == "/v1/node/configuration" || path == "/v1/node/manage" || path == "/v1/node/receipt" || path == "/v1/node/proof" || path == "/v1/node/owned-runtime-settings" || path == "/v1/node/managed-product" || path == "/v1/node/managed-disable" || path == "/v1/node/managed-status" || path == "/v1/node/managed-start" || path == "/v1/node/managed-product-proxy" || path == "/v1/node/managed-disable-receipt")
}
func strictDecode(r io.Reader, v any) error {
	d := json.NewDecoder(io.LimitReader(r, productrpc.MaxCommandBytes+1))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid node request")
	}
	return nil
}

func Handler(agent nodeplane.CatalogAgent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed(r.Method, r.URL.RequestURI()) {
			http.Error(w, "node endpoint unavailable", 404)
			return
		}
		var value any
		var err error
		switch r.URL.Path {
		case "/v1/node/catalog":
			value, err = agent.Catalog(r.Context())
		case "/v1/node/owned-runtime-settings":
			var input ownedRuntimeSettingsRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ownedRuntimeSettingsPort)
			if !ok {
				http.Error(w, "native runtime settings unavailable", 503)
				return
			}
			value, err = port.ReadOwnedRuntimeSettings(r.Context(), input.NodeID, input.Backend)
		case "/v1/node/configuration":
			var input struct {
				NodeID  string          `json:"nodeId"`
				Backend api.NodeBackend `json:"backend"`
			}
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			value, err = agent.Configuration(r.Context(), input.NodeID, input.Backend)
		case "/v1/node/proof":
			var input api.WorkTarget
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(nodeplane.RuntimeProofPort)
			if !ok {
				http.Error(w, "managed runtime owner unavailable", 503)
				return
			}
			value, err = port.ReadRuntimeProof(r.Context(), input)
		case "/v1/node/managed-product":
			var input api.WorkTarget
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedProductPort)
			if !ok {
				http.Error(w, "managed product unavailable", 503)
				return
			}
			value, err = port.ReadManagedProduct(r.Context(), input)
		case "/v1/node/managed-disable":
			var input ManagedDisableRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedProductPort)
			if !ok {
				http.Error(w, "managed control unavailable", 503)
				return
			}
			value, err = port.PrepareManagedDisable(r.Context(), input)
		case "/v1/node/managed-status":
			var input struct {
				NodeID string `json:"nodeId"`
			}
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedStartPort)
			if !ok {
				http.Error(w, "managed start unavailable", 503)
				return
			}
			value, err = port.ManagedRoamingStatus(r.Context(), input.NodeID)
		case "/v1/node/managed-start":
			var input ManagedStartRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedStartPort)
			if !ok {
				http.Error(w, "managed start unavailable", 503)
				return
			}
			value, err = port.StartManagedRoaming(r.Context(), input)
		case "/v1/node/managed-product-proxy":
			var input ManagedProductRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedProductPort)
			if !ok {
				http.Error(w, "managed proxy unavailable", 503)
				return
			}
			value, err = port.ProxyManagedProduct(r.Context(), input)
		case "/v1/node/managed-disable-receipt":
			var input ManagedDisableRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			port, ok := agent.(ManagedProductPort)
			if !ok {
				http.Error(w, "managed receipt unavailable", 503)
				return
			}
			value, err = port.ReconcileManagedDisable(r.Context(), input)
		case "/v1/node/manage":
			var input nodeplane.ManagementRequest
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			value, err = agent.Manage(r.Context(), input)
		case "/v1/node/receipt":
			var input api.NodeOperationRef
			if strictDecode(r.Body, &input) != nil {
				http.Error(w, "invalid node request", 400)
				return
			}
			value, err = agent.Reconcile(r.Context(), input)
		}
		if err != nil {
			http.Error(w, "node operation unavailable", 503)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	})
}

// Serve accepts private authenticated native streams; closing one stream only
// detaches that observer. Listener permissions are established by native owner.
func Serve(ctx context.Context, listener net.Listener, agent nodeplane.CatalogAgent) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	var workers sync.WaitGroup
	stop := context.AfterFunc(ctx, func() {
		_ = listener.Close()
		mu.Lock()
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
	})
	defer func() {
		cancel()
		stop()
		mu.Lock()
		for c := range connections {
			_ = c.Close()
		}
		mu.Unlock()
		workers.Wait()
	}()
	handler := Handler(agent)
	for {
		c, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		if len(connections) >= 32 {
			mu.Unlock()
			_ = c.Close()
			continue
		}
		connections[c] = true
		workers.Add(1)
		mu.Unlock()
		go func() {
			defer workers.Done()
			defer c.Close()
			defer func() { mu.Lock(); delete(connections, c); mu.Unlock() }()
			_ = productrpc.ServeNativeStream(ctx, c, c, handler, allowed)
		}()
	}
}

type Client struct {
	expected  string
	http      *http.Client
	transport productrpc.NativeStreamTransport
}

var _ nodeplane.CatalogAgent = (*Client)(nil)

func NewClient(expectedNode string, stream io.ReadWriteCloser) (*Client, error) {
	if !identifier.MatchString(expectedNode) {
		return nil, errors.New("explicit inspected node identity required")
	}
	t, err := productrpc.NewNativeStreamTransport(stream, allowed)
	if err != nil {
		return nil, err
	}
	return &Client{expected: expectedNode, http: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("node redirect refused") }}, transport: t}, nil
}
func (c *Client) Close() error { c.transport.CloseIdleConnections(); return nil }
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil || len(body) > productrpc.MaxCommandBytes {
			return errors.New("node request limit")
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	h, err := c.http.Do(r)
	if err != nil {
		return errors.New("node stream unavailable")
	}
	defer h.Body.Close()
	if h.StatusCode != 200 {
		return errors.New("node operation unavailable")
	}
	d := json.NewDecoder(io.LimitReader(h.Body, productrpc.MaxSnapshotBytes+1))
	d.DisallowUnknownFields()
	if d.Decode(output) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid node response")
	}
	return nil
}
func (c *Client) Catalog(ctx context.Context) (api.NodeCatalog, error) {
	var out api.NodeCatalog
	err := c.request(ctx, "GET", "/v1/node/catalog", nil, &out)
	if err == nil && (len(out.Nodes) != 1 || out.Nodes[0].ID != c.expected || len(out.Revision) != 64) {
		err = errors.New("node identity changed")
	}
	return out, err
}
func (c *Client) Configuration(ctx context.Context, nodeID string, b api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	var out api.NodeRuntimeConfiguration
	if nodeID != c.expected || !backend(b) {
		return out, errors.New("node configuration scope changed")
	}
	err := c.request(ctx, "POST", "/v1/node/configuration", struct {
		NodeID  string          `json:"nodeId"`
		Backend api.NodeBackend `json:"backend"`
	}{nodeID, b}, &out)
	if err == nil && (out.Guard.NodeID != nodeID || out.Guard.Backend != b) {
		err = errors.New("node configuration scope changed")
	}
	return out, err
}
func (c *Client) Manage(ctx context.Context, r nodeplane.ManagementRequest) (api.NodeOperationReceipt, error) {
	out := api.NodeOperationReceipt{Ref: r.Ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}
	if r.Ref.NodeID != c.expected {
		return out, errors.New("node mutation scope changed")
	}
	err := c.request(ctx, "POST", "/v1/node/manage", r, &out)
	if err == nil && out.Ref != r.Ref {
		err = errors.New("node receipt identity changed")
	}
	if err != nil {
		out = api.NodeOperationReceipt{Ref: r.Ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}
	}
	return out, err
}
func (c *Client) Reconcile(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	out := api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}
	if ref.NodeID != c.expected {
		return out, errors.New("node receipt scope changed")
	}
	err := c.request(ctx, "POST", "/v1/node/receipt", ref, &out)
	if err == nil && out.Ref != ref {
		err = errors.New("node receipt identity changed")
	}
	if err != nil {
		out = api.NodeOperationReceipt{Ref: ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}
	}
	return out, err
}
func Dial(ctx context.Context, socket, expectedNode string) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := CheckPrivateDirectory(filepath.Dir(socket)); err != nil {
		return nil, err
	}
	connection, err := localipc.Dial(socket, 10*time.Second)
	if err != nil {
		return nil, errors.New("private node socket unavailable")
	}
	c, err := NewClient(expectedNode, connection)
	if err != nil {
		connection.Close()
	}
	return c, err
}

type sshStream struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
	once   sync.Once
}

func (s *sshStream) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *sshStream) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *sshStream) Close() error {
	s.once.Do(func() {
		_ = s.stdin.Close()
		_ = s.stdout.Close()
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		_ = s.cmd.Wait()
	})
	return nil
}
func NewSSHClient(ctx context.Context, s SSHConfig, helper, socket, expectedNode string) (*Client, error) {
	args, err := s.args()
	if err != nil || !filepath.IsAbs(helper) || !validSocket(socket) {
		return nil, errors.New("invalid SSH node proxy")
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, shellQuote(helper)+" proxy-agent --socket "+shellQuote(socket))
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if cmd.Start() != nil {
		in.Close()
		out.Close()
		return nil, errors.New("SSH node proxy unavailable")
	}
	stream := &sshStream{stdin: in, stdout: out, cmd: cmd}
	c, err := NewClient(expectedNode, stream)
	if err != nil {
		stream.Close()
	}
	return c, err
}

func (c *Client) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	var out nodeplane.RuntimeEligibility
	if target.NodeID != c.expected || target.Role != api.RoleBot || target.Validate() != nil {
		return out, errors.New("runtime proof scope changed")
	}
	err := c.request(ctx, "POST", "/v1/node/proof", target, &out)
	if err == nil && (out.Proof.NodeID != target.NodeID || string(out.Proof.Backend) != target.Backend || !out.Proof.Controllable) {
		err = errors.New("native runtime proof scope changed")
	}
	return out, err
}

// NewSSHForegroundClient starts only an explicitly requested lightweight
// foreground agent. Its SSH lifetime ends on Close; no persistent service or
// Runtime/Bot process is created. Receipt/identity files remain target-local.
func NewSSHForegroundClient(ctx context.Context, s SSHConfig, helper, directory, expectedNode string) (*Client, error) {
	args, err := s.args()
	if err != nil || !filepath.IsAbs(helper) || !filepath.IsAbs(directory) || strings.ContainsAny(helper+directory, "\x00\r\n") {
		return nil, errors.New("invalid foreground SSH node agent")
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, shellQuote(helper)+" serve-agent --stdio --directory "+shellQuote(directory)+" --runtime-directory "+shellQuote(filepath.Join(directory, "runtime"))+" --node-id "+shellQuote(expectedNode))
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if cmd.Start() != nil {
		in.Close()
		out.Close()
		return nil, errors.New("foreground SSH node agent unavailable")
	}
	stream := &sshStream{stdin: in, stdout: out, cmd: cmd}
	c, err := NewClient(expectedNode, stream)
	if err != nil {
		stream.Close()
	}
	return c, err
}

// OpenBrokerStream owns only the strict existing SSH observation process for
// the closed target-local broker helper. It neither creates a broker nor adds
// forwarding, credentials, key material or account configuration.
func OpenBrokerStream(ctx context.Context, s SSHConfig, helper, socket string) (io.ReadWriteCloser, error) {
	args, err := s.args()
	if err != nil || !filepath.IsAbs(helper) || !validSocket(socket) || strings.ContainsAny(helper, "\x00\r\n") {
		return nil, errors.New("invalid SSH broker proxy")
	}
	args = append(args, "-o", "ClearAllForwardings=yes", "--", s.Target, shellQuote(helper)+" proxy-broker --socket "+shellQuote(socket))
	cmd := exec.CommandContext(ctx, s.binary(), args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if cmd.Start() != nil {
		in.Close()
		out.Close()
		return nil, errors.New("SSH broker proxy unavailable")
	}
	return &sshStream{stdin: in, stdout: out, cmd: cmd}, nil
}
