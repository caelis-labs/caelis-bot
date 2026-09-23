package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type testEngine struct {
	api.Engine
	mu                sync.Mutex
	tools             *api.ToolConnection
	connected, closed int
	bindErr           error
	connectSeen       chan struct{}
	snapshots         chan api.Snapshot
}

func newTestEngine() *testEngine {
	return &testEngine{connectSeen: make(chan struct{}), snapshots: make(chan api.Snapshot)}
}
func (*testEngine) ProviderInfo() api.ProviderInfo {
	return api.ProviderInfo{ID: "fixture", Name: "Fixture"}
}
func (*testEngine) Snapshot() api.Snapshot { return api.Snapshot{} }
func (e *testEngine) Connect(ctx context.Context) error {
	e.mu.Lock()
	if e.tools == nil {
		e.mu.Unlock()
		return errors.New("tools were not bound before connect")
	}
	e.connected++
	if e.connected == 1 {
		close(e.connectSeen)
	}
	e.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}
func (e *testEngine) WaitSnapshot(ctx context.Context, _ uint64) (api.Snapshot, error) {
	select {
	case s := <-e.snapshots:
		return s, nil
	case <-ctx.Done():
		return api.Snapshot{}, ctx.Err()
	}
}
func (e *testEngine) ConfigureBotTools(c *api.ToolConnection) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tools = c.Clone()
	return e.bindErr
}
func (e *testEngine) Close(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed++
	return nil
}
func (*testEngine) ListTasks() []api.Task { return nil }
func (*testEngine) StartTask(context.Context, api.TaskStart) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) ReadTask(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) SendTask(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) StopTask(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) DeliverTaskReport(context.Context) error { return nil }

func fixtureApp(t *testing.T, e api.Engine, host Host) (*Application, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"runtime":"fixture","cliPath":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := newApplication(root, host, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(c providerConfig) (api.Engine, error) {
			if c.ConversationFile != filepath.Join(root, "providers", "fixture", "conversation.json") {
				t.Fatal("provider state reused legacy namespace")
			}
			return e, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, root
}
func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle did not advance")
	}
}
func TestProductLifecycleBindsBeforeConnectObservesAndClosesOnce(t *testing.T) {
	e := newTestEngine()
	observed := make(chan struct{})
	a, _ := fixtureApp(t, e, Host{Observe: func(s api.Snapshot) {
		if s.Revision != 42 {
			t.Error("wrong revision")
		}
		close(observed)
	}})
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, e.connectSeen)
	e.snapshots <- api.Snapshot{Revision: 42}
	waitSignal(t, observed)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := a.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.connected != 1 || e.closed != 1 {
		t.Fatal("duplicate connection/cleanup", e.connected, e.closed)
	}
	if _, err := os.Stat(e.tools.Env["CAELIS_BOT_ENDPOINT"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private endpoint leaked")
	}
	if err := a.Start(); err == nil {
		t.Fatal("closed product restarted")
	}
}
func TestCloseBeforeNativeReadyDoesNotCreateResidentState(t *testing.T) {
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err == nil {
		t.Fatal("started after shutdown")
	}
	if _, err := os.Stat(filepath.Join(root, "bot.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("premature resident state")
	}
	if e.connected != 0 || e.closed != 1 {
		t.Fatal("incorrect lifecycle")
	}
}
func TestToolBindingFailureDoesNotConnectAndReleasesEndpoint(t *testing.T) {
	e := newTestEngine()
	e.bindErr = errors.New("unsupported tool binding")
	a, _ := fixtureApp(t, e, Host{})
	if err := a.Start(); err == nil {
		t.Fatal("ignored missing tools")
	}
	if e.connected != 0 {
		t.Fatal("connected without tools")
	}
	if _, err := os.Stat(e.tools.Env["CAELIS_BOT_ENDPOINT"]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed startup leaked endpoint")
	}
}
func TestUnknownProviderNeverFallsBackOrRewritesBindings(t *testing.T) {
	root := t.TempDir()
	settings := []byte(`{"runtime":"not-implemented","cliPath":""}`)
	sentinel := []byte(`{"version":1,"threadId":"existing-owned-thread"}`)
	for name, value := range map[string][]byte{"runtime.json": settings, "conversation.json": sentinel} {
		if err := os.WriteFile(filepath.Join(root, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(root, Host{}); err == nil {
		t.Fatal("unimplemented provider admitted")
	}
	for name, value := range map[string][]byte{"runtime.json": settings, "conversation.json": sentinel} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(got) != string(value) {
			t.Fatal("unknown provider changed state")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "bot.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created resident on failed admission")
	}
}
func TestProviderNamespacesAndLegacyCodexRestoration(t *testing.T) {
	root := t.TempDir()
	if _, err := providerDirectory(root, "../codex"); err == nil {
		t.Fatal("path injection")
	}
	for _, id := range []string{"codex", "fixture"} {
		got, err := providerDirectory(root, id)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, "providers", id)
		if id == "codex" {
			want = root
		}
		if got != want {
			t.Fatal("wrong namespace", id, got)
		}
	}
	existing := []byte(`{"version":1,"threadId":"existing-owned-thread","tasks":{}}`)
	if err := os.WriteFile(filepath.Join(root, "conversation.json"), existing, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prefs, prefErr := a.Backend.ExecutionSettings()
	if a.Backend.ProviderInfo().ID != "codex" || prefErr != nil || prefs.ApprovalMode != "auto" {
		t.Fatal("legacy defaults changed")
	}
	got, err := os.ReadFile(filepath.Join(root, "conversation.json"))
	if err != nil || string(got) != string(existing) {
		t.Fatal("startup rewrote live bindings")
	}
}
func TestChatOnlyAdapterCannotBecomeSecretary(t *testing.T) {
	// Expose only the baseline engine and identity, hiding the delegation ports.
	e := newTestEngine()
	limited := struct {
		api.Engine
		api.Provider
	}{e, e}
	if err := requireAssistant(limited, "fixture"); err == nil {
		t.Fatal("chat-only adapter advertised as a secretary")
	}
	if err := requireAssistant(e, "other"); err == nil {
		t.Fatal("mismatched provider identity")
	}
}

// A Control-owned companion must not receive Codex MCP or its scheduler.
type controlTestEngine struct {
	*testEngine
	effects api.DesktopEffects
}

func (e *controlTestEngine) BindDesktop(v api.DesktopEffects) error       { e.effects = v; return nil }
func (*controlTestEngine) OwnedTasks(context.Context) ([]api.Task, error) { return nil, nil }
func (e *controlTestEngine) Connect(ctx context.Context) error {
	if e.effects.Execute == nil {
		return errors.New("desktop effects missing")
	}
	close(e.connectSeen)
	<-ctx.Done()
	return ctx.Err()
}
func TestControlCompanionHasOneExecutionOwner(t *testing.T) {
	e := &controlTestEngine{testEngine: newTestEngine()}
	a, root := fixtureApp(t, e, Host{})
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, e.connectSeen)
	if a.bridge != nil || e.tools != nil {
		t.Fatal("Control companion received a second tool server")
	}
	if _, err := e.effects.Execute("clock", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.effects.Execute("reminders", json.RawMessage(`{"operation":"save","id":"native","label":"Fixture","prompt":"Fixture","at":"2099-01-01T00:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "providers", "fixture", "bot.json")); err != nil {
		t.Fatal("missing private provider schedule", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bot.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Control mutated Codex resident state")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
