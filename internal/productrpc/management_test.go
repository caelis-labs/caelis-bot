package productrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type fakeManagement struct {
	mu                          sync.Mutex
	scope                       productmanagement.Scope
	configs, installs, resolves int
	outcome                     string
	entered, release            chan struct{}
	cancelled                   bool
}

func (*fakeManagement) Capabilities() productmanagement.Capabilities {
	return productmanagement.Capabilities{Installation: true, Configuration: true}
}
func (*fakeManagement) ReviewedReleases(productmanagement.Scope) ([]productmanagement.ReviewedRelease, error) {
	return []productmanagement.ReviewedRelease{{Runtime: "caelis", Version: "0.65.0"}}, nil
}
func (*fakeManagement) RuntimeStatus(_ context.Context, _ productmanagement.Scope, runtime string) (runtimemanagement.Status, error) {
	return runtimemanagement.Status{Runtime: runtime, Outcome: "accepted"}, nil
}
func (*fakeManagement) RuntimeConfiguration(context.Context, productmanagement.Scope) (api.RuntimeConfiguration, error) {
	return api.RuntimeConfiguration{Revision: "7", Main: api.WorkExecutionSettings{Model: "public-model"}}, nil
}
func (f *fakeManagement) ManageRuntime(_ context.Context, c productmanagement.RuntimeCommand) (productmanagement.RuntimeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	outcome := "unknown"
	if c.Action == "resolve" {
		f.resolves++
		outcome = "accepted"
	} else {
		f.installs++
	}
	return productmanagement.RuntimeResult{Scope: f.scope, ID: c.ID, Outcome: outcome, Status: runtimemanagement.Status{Runtime: c.Runtime, RequestID: c.ID, Outcome: outcome, Version: c.Version}}, nil
}
func (f *fakeManagement) ChangeRuntimeConfiguration(ctx context.Context, c productmanagement.ConfigurationCommand) (productmanagement.ConfigurationResult, error) {
	f.mu.Lock()
	f.configs++
	entered, release := f.entered, f.release
	outcome := f.outcome
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = true
			f.mu.Unlock()
			return productmanagement.ConfigurationResult{}, ctx.Err()
		}
	}
	native := "committed"
	if outcome == "unknown" {
		native = "unknown"
	}
	return productmanagement.ConfigurationResult{Scope: f.scope, ID: c.ID, Outcome: outcome, Native: api.RuntimeMutationResult{OperationID: "raw-Core-operation-17", Outcome: native, Message: "Native configuration received"}}, nil
}
func managementFixture(t *testing.T) (*Server, *Client, *fakeManagement, Options) {
	t.Helper()
	m := &fakeManagement{outcome: "accepted"}
	opts := Options{NodeID: "node", BotID: "bot", Token: strings.Repeat("t", 64), JournalFile: filepath.Join(t.TempDir(), "receipts.json"), Management: func(scope productmanagement.Scope) (productmanagement.Port, error) { m.scope = scope; return m, nil }}
	s, err := NewServer(&fakeProduct{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	t.Cleanup(h.Close)
	c, err := NewClient(ClientOptions{URL: h.URL, ExpectedNode: "node", ExpectedBot: "bot", Token: opts.Token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	if _, err = c.Connect(bounded(t)); err != nil {
		t.Fatal(err)
	}
	return s, c, m, opts
}
func configIntent(id string) productmanagement.ConfigurationCommand {
	return productmanagement.ConfigurationCommand{ID: id, Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "7", Selection: api.WorkExecutionSettings{Model: "public-model"}}}
}

func TestManagementTypedStdioAndNativeIDPrivacy(t *testing.T) {
	s, c, _, opts := managementFixture(t)
	if !s.Identity().Capabilities.RuntimeManagement {
		t.Fatal("bound management hidden")
	}
	h := httptest.NewServer(s)
	defer h.Close()
	left, right := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = ProxyStdio(ctx, right, right, h.URL, opts.Token) }()
	remote, err := NewStdioClient(StdioOptions{ExpectedNode: "node", ExpectedBot: "bot"}, left)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if _, err = remote.Connect(bounded(t)); err != nil {
		t.Fatal(err)
	}
	caps, err := remote.ManagementCapabilities(bounded(t))
	if err != nil || !caps.Installation || !caps.Configuration || caps.ConfigurationReceiptLookup {
		t.Fatal(caps, err)
	}
	releases, err := remote.ReviewedReleases(bounded(t))
	if err != nil || len(releases) != 1 {
		t.Fatal(releases, err)
	}
	status, err := remote.RuntimeStatus(bounded(t), "caelis")
	if err != nil || status.Runtime != "caelis" {
		t.Fatal(status, err)
	}
	current, err := remote.RuntimeConfiguration(bounded(t))
	if err != nil || current.Revision != "7" {
		t.Fatal(current, err)
	}
	result, err := remote.ChangeRuntimeConfiguration(bounded(t), configIntent("configuration-committed"))
	if err != nil || result.Outcome != "accepted" || result.Native.Outcome != "committed" || result.Native.OperationID != "" {
		t.Fatal(result, err)
	}
	r, err := c.Receipt(bounded(t), result.ID)
	if err != nil || r.Configuration == nil || r.Configuration.Native.Outcome != "committed" || r.Configuration.Native.OperationID != "" {
		t.Fatal(r, err)
	}
	b, err := os.ReadFile(opts.JournalFile)
	if err != nil || bytes.Contains(b, []byte("raw-Core-operation")) || bytes.Contains(b, []byte("Native configuration received")) {
		t.Fatal("private native receipt entered product journal", err)
	}
}

func TestManagementCancellationUnknownReceiptNeverRedispatches(t *testing.T) {
	s, c, m, opts := managementFixture(t)
	m.outcome = "unknown"
	m.entered = make(chan struct{})
	m.release = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan productmanagement.ConfigurationResult, 1)
	go func() { r, _ := c.ChangeRuntimeConfiguration(ctx, configIntent("configuration-unknown")); done <- r }()
	select {
	case <-m.entered:
	case <-bounded(t).Done():
		t.Fatal("native configuration never admitted")
	}
	cancel()
	select {
	case r := <-done:
		if r.ID != "configuration-unknown" || r.Outcome != "unknown" {
			t.Fatal(r)
		}
	case <-bounded(t).Done():
		t.Fatal("canceled observer blocked")
	}
	close(m.release)
	// Drain the same serialized native operation, rather than poll/sleep.
	for {
		r, err := c.Receipt(bounded(t), "configuration-unknown")
		if err != nil {
			t.Fatal(err)
		}
		if r.Code != "pending" {
			if r.Outcome != "unknown" || r.Configuration == nil || r.Configuration.Native.Outcome != "unknown" {
				t.Fatal(r)
			}
			break
		}
		s.commands.Lock()
		s.commands.Unlock()
	}
	m.mu.Lock()
	calls, cancelled := m.configs, m.cancelled
	m.mu.Unlock()
	if calls != 1 || cancelled {
		t.Fatal("observer cancellation changed native intent", calls, cancelled)
	}
	// A later matching configuration value is never proof of this operation.
	_, _ = c.RuntimeConfiguration(bounded(t))
	r, err := c.ChangeRuntimeConfiguration(bounded(t), configIntent("configuration-unknown"))
	if err != nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	restarted, err := NewServer(&fakeProduct{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(restarted)
	defer h.Close()
	next, err := NewClient(ClientOptions{URL: h.URL, Token: opts.Token, ExpectedNode: "node", ExpectedBot: "bot"})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	_, _ = next.Connect(bounded(t))
	r, err = next.ChangeRuntimeConfiguration(bounded(t), configIntent("configuration-unknown"))
	if err == nil || r.Outcome != "unknown" {
		t.Fatal("old receipt scope must require explicit receipt lookup", r, err)
	}
	receipt, err := next.Receipt(bounded(t), r.ID)
	if err != nil || receipt.Outcome != "unknown" {
		t.Fatal(receipt, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.configs != 1 {
		t.Fatal("unknown configuration replayed", m.configs)
	}
}

func TestManagementResolveOriginalIntentAndGenerationFence(t *testing.T) {
	s, c, m, opts := managementFixture(t)
	intent := productmanagement.RuntimeCommand{ID: "install-original", Action: "install", Runtime: "caelis", Version: "0.65.0"}
	r, err := c.ManageRuntime(bounded(t), intent)
	if err != nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	resolve := intent
	resolve.Action = "resolve"
	changed := resolve
	changed.Version = "0.99.0"
	if _, err = c.ManageRuntime(bounded(t), changed); err == nil {
		t.Fatal("changed original intent resolved")
	}
	r, err = c.ManageRuntime(bounded(t), resolve)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	_, err = c.ManageRuntime(bounded(t), resolve)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	installs, resolves := m.installs, m.resolves
	m.mu.Unlock()
	if installs != 1 || resolves != 1 {
		t.Fatal("resolve replayed operation", installs, resolves)
	}
	// A persisted unknown original generation cannot be rehashed into new native authority.
	original := productmanagement.RuntimeCommand{ID: "install-old-generation", Action: "install", Runtime: "caelis", Version: "0.65.0"}
	if _, err = c.ManageRuntime(bounded(t), original); err != nil {
		t.Fatal(err)
	}
	s.commands.Lock()
	s.commands.Unlock()
	restarted, err := NewServer(&fakeProduct{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(restarted)
	defer h.Close()
	next, _ := NewClient(ClientOptions{URL: h.URL, Token: opts.Token, ExpectedNode: "node", ExpectedBot: "bot"})
	defer next.Close()
	_, _ = next.Connect(bounded(t))
	original.Action = "resolve"
	r, err = next.ManageRuntime(bounded(t), original)
	if err != nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.installs != 2 || m.resolves != 1 {
		t.Fatal("restart adopted original native scope")
	}
}

func TestManagementClosedSchemaAuthAndUnavailable(t *testing.T) {
	s, c, m, opts := managementFixture(t)
	for _, payload := range []string{
		`{"action":"connect-model","expectedRevision":"7","apiKey":"SECRET"}`,
		`{"action":"main","expectedRevision":"7","selection":{"model":"public","token":"SECRET"}}`,
		`{"action":"main","expectedRevision":"7","selection":{"model":"public"},"url":"https://example.com"}`,
	} {
		var value map[string]any
		_ = json.Unmarshal([]byte(payload), &value)
		command := map[string]any{"botId": s.identity.BotID, "generation": s.identity.Generation, "id": "secret-attempt", "kind": "configure-runtime", "configuration": map[string]any{"botId": s.identity.BotID, "generation": s.identity.Generation, "id": "secret-attempt", "change": value}}
		b, _ := json.Marshal(command)
		request := httptest.NewRequest("POST", "/v1/commands", bytes.NewReader(b))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+opts.Token)
		response := httptest.NewRecorder()
		s.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatal("secret/connection schema accepted", response.Code)
		}
	}
	scope := s.identity.Scope
	scope.Generation = "stale-generation"
	b, _ := json.Marshal(scope)
	request := httptest.NewRequest("POST", "/v1/management/configuration", bytes.NewReader(b))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+opts.Token)
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 409 {
		t.Fatal("stale read admitted")
	}
	request = httptest.NewRequest("POST", "/v1/management/capabilities", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	s.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("unauthenticated settings exposed")
	}
	if _, err := c.ManageRuntime(bounded(t), productmanagement.RuntimeCommand{ID: "bad", Action: "install", Runtime: "caelis"}); err == nil {
		t.Fatal("unbounded incomplete installer intent accepted")
	}
	m.mu.Lock()
	calls := m.configs + m.installs
	m.mu.Unlock()
	if calls != 0 {
		t.Fatal("invalid schema dispatched")
	}
	_, local, _, _, _ := fixture(t)
	caps, err := local.ManagementCapabilities(bounded(t))
	if err != nil || caps.Installation || caps.Configuration {
		t.Fatal("unbound capability advertised", caps, err)
	}
	if _, err = local.RuntimeConfiguration(bounded(t)); err == nil {
		t.Fatal("unbound configuration read succeeded")
	}
	// The proxy cannot widen this closed list to connection/auth/control paths.
	for _, path := range []string{"/v1/management/connection", "/api/control/v1/status", "/v1/management/configuration?url=foo"} {
		if validProxyRequest(proxyFrame{ID: "1", Method: "POST", Path: path}) {
			t.Fatal("unsafe management route admitted", path)
		}
	}
}
