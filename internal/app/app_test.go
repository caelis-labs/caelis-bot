package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
func (*testEngine) WorkAdmission(context.Context) error { return nil }
func (*testEngine) WorkStates() []api.WorkState         { return nil }
func (*testEngine) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) ReadWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) SendWork(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) StopWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("not used")
}
func (*testEngine) SubmitReport(context.Context, api.Submission) (api.Receipt, error) {
	return api.Receipt{}, errors.New("not used")
}

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

// A legacy Control companion cannot bypass the application's work/tool ports.
func TestLegacyCompanionCannotBypassProductAssembly(t *testing.T) {
	e := newTestEngine()
	legacy := struct {
		api.Engine
		api.Provider
		api.SnapshotObserver
		api.ControlCompanion
	}{Engine: e, Provider: e, SnapshotObserver: e}
	if err := requireAssistant(legacy, "fixture"); err == nil {
		t.Fatal("legacy product owner bypassed generic ports")
	}
}

func TestPersonalDataBeforeRuntimeChoiceDoesNotStartExecution(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if a.HasRuntimeChoice() || a.started || a.bridge != nil {
		t.Fatal("local personal data selected or started a runtime")
	}
	err = os.WriteFile(filepath.Join(a.notebook.Path(), "offline.md"), []byte("local data"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.HasRuntimeChoice() || !b.NeedsSetup() {
		t.Fatal("offline identity bypassed onboarding")
	}
	if err = b.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	n, err := os.ReadFile(filepath.Join(b.notebook.Path(), "offline.md"))
	if err != nil || string(n) != "local data" {
		t.Fatal("offline note lost", err)
	}
}

func TestPersonalInitializationPreservesLegacyRuntimeChoice(t *testing.T) {
	root := t.TempDir()
	legacy := []byte(`{"version":1,"id":"legacy-bot","schedules":[]}`)
	if err := os.WriteFile(filepath.Join(root, "bot.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasRuntimeChoice() {
		t.Fatal("lost legacy Codex choice")
	}
	if err = a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.HasRuntimeChoice() || b.setup.Overview().Onboarding || !b.Backend.BotInitialization().Required {
		t.Fatal("upgrade lost existing runtime selection")
	}
	if err = b.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if b.companion.State().ID != "legacy-bot" || b.companion.State().PersonalVersion != 1 {
		t.Fatal("upgrade replaced identity or omitted capability version")
	}
}

func TestNotebookSkillIsResidentOnlyAndRegeneratesExternalNotes(t *testing.T) {
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	defer a.Close()
	// Native startup prepares local data before connecting; repeated preparation
	// must not close the Vault handle that the running adapter will use.
	for range 2 {
		if err := a.PreparePersonal(); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	binding := e.tools.Clone()
	e.mu.Unlock()
	if binding.NotebookDirectory != filepath.Join(root, "Notebook") || !strings.Contains(binding.Instructions, a.skillPath) {
		t.Fatal("Notebook/skill not bound")
	}
	if strings.Contains(binding.WorkerInstructions, "Notebook") || strings.Contains(binding.WorkerInstructions, a.skillPath) {
		t.Fatal("skill forwarded to worker")
	}
	content, err := os.ReadFile(a.skillPath)
	if err != nil || !strings.Contains(string(content), "MEMORY.md") {
		t.Fatal("packaged skill missing", err)
	}
	note := filepath.Join(binding.NotebookDirectory, "external.md")
	if err = os.WriteFile(note, []byte("# External note\nNot a private format."), 0600); err != nil {
		t.Fatal(err)
	}
	if err = binding.PrepareTurn(t.Context()); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(binding.NotebookDirectory, "INDEX.md"))
	if !strings.Contains(string(body), "External note") {
		t.Fatal("external edit not indexed")
	}
	os.Remove(note)
	binding.FinishTurn()
	body, _ = os.ReadFile(filepath.Join(binding.NotebookDirectory, "INDEX.md"))
	if strings.Contains(string(body), "External note") {
		t.Fatal("deleted note not removed from index")
	}
}
