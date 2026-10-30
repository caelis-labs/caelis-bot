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
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestControlConversationUsesLocalIMAndMasksMarkedSecret(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	first := textchannel.Inbound{Channel: "telegram", Conversation: "owner", ID: "one", Text: "/unknown"}
	if got := a.textControl.Handle(t.Context(), first, api.Snapshot{}); !strings.Contains(got, "未知命令") {
		t.Fatal(got)
	}
	approval := api.Approval{ID: "native", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer", Label: "提交"}}, Questions: []api.Question{
		{ID: "secret", Title: "密钥", Type: "text", Required: true, Secret: true},
		{ID: "reason", Title: "用途", Type: "text", Required: true},
	}}
	if _, _, err := a.textControl.Card(approval); err != nil {
		t.Fatal(err)
	}
	const secret = "IM_SECRET_SENTINEL"
	in := textchannel.Inbound{Channel: "telegram", Conversation: "owner", ID: "two", Text: "/answer Q1 " + secret}
	if got := a.textControl.Handle(t.Context(), in, api.Snapshot{Approvals: []api.Approval{approval}}); !strings.Contains(got, "仍需回答") {
		t.Fatal(got)
	}
	assertDisplay := func(items []api.Item) {
		t.Helper()
		var visible string
		for _, item := range items {
			visible += item.Text + "\n"
		}
		if len(items) != 4 || !strings.Contains(visible, "/unknown") || !strings.Contains(visible, "已填写敏感字段") || strings.Contains(visible, secret) {
			t.Fatalf("local IM control projection: %q", visible)
		}
	}
	assertDisplay(waitChatInputs(t, func() []api.Item { return a.Backend.ChatSnapshot(0, "", "").Snapshot.Items }, first.ID, in.ID))
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(root, "chat.sqlite"))
	if err != nil || strings.Contains(string(disk), secret) {
		t.Fatal("marked secret entered local IM storage", err)
	}
	reopened := chatlog.Open(filepath.Join(root, "chat.sqlite"))
	defer reopened.Close()
	assertDisplay(waitChatInputs(t, func() []api.Item { items, _ := reopened.Snapshot(); return items }, first.ID, in.ID))
}

// New user items become visible only after the chat writer checks persisted
// request identity. Wait for those IDs, then assert their bodies separately.
func waitChatInputs(t *testing.T, read func() []api.Item, requestIDs ...string) []api.Item {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items := read()
		found := 0
		for _, id := range requestIDs {
			for _, item := range items {
				if item.Kind == "user" && item.RequestID == id {
					found++
					break
				}
			}
		}
		if found == len(requestIDs) {
			return items
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("accepted chat inputs did not become visible: %v", requestIDs)
	return nil
}

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
func (*testEngine) SubmitSystem(context.Context, api.SystemMessage) (api.Receipt, error) {
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

func TestCloseReleasesLifecycleLockWhileWorkersDrain(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	begin, stop, acquired := make(chan struct{}), make(chan struct{}), make(chan struct{})
	// Cleanup runs before fixtureApp's Close cleanup, including on assertion
	// failure. A stuck old Close can then finish rather than hanging the test.
	t.Cleanup(func() { close(stop) })
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		select {
		case <-begin:
		case <-stop:
			return
		}
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			if a.lifecycleMu.TryLock() {
				a.lifecycleMu.Unlock()
				close(acquired)
				return
			}
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	closed := make(chan error, 1)
	go func() { closed <- a.Close() }()
	deadline := time.After(2 * time.Second)
	for {
		a.mu.Lock()
		stopping := a.closed
		a.mu.Unlock()
		if stopping {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Close did not begin")
		case <-time.After(time.Millisecond):
		}
	}
	close(begin)
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("Close held lifecycleMu while waiting for a worker")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after its worker exited")
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
func TestUnknownProviderKeepsShellWithoutAdmittingGuessedOwnerOrRewritingBindings(t *testing.T) {
	root := t.TempDir()
	settings := []byte(`{"runtime":"not-implemented","cliPath":""}`)
	sentinel := []byte(`{"version":1,"threadId":"existing-owned-thread"}`)
	for name, value := range map[string][]byte{"runtime.json": settings, "conversation.json": sentinel} {
		if err := os.WriteFile(filepath.Join(root, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal("unsupported provider disabled shell", err)
	}
	defer a.Close()
	if err := a.Backend.Connect(t.Context()); err == nil {
		t.Fatal("unimplemented provider admitted as a native execution owner")
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
	if binding.NotebookDirectory != filepath.Join(root, "Notebook") || !strings.Contains(binding.SkillInstructions, a.skillPath) || !strings.Contains(binding.Instructions, "read the bot-core Skill") || strings.Contains(binding.Instructions, a.skillPath) || strings.Contains(binding.WorkerInstructions, "bot-core") {
		t.Fatal("Notebook/skill not bound")
	}
	if strings.Contains(binding.WorkerInstructions, "Notebook") || strings.Contains(binding.WorkerInstructions, a.skillPath) {
		t.Fatal("skill forwarded to worker")
	}
	content, err := os.ReadFile(a.skillPath)
	if err != nil || !strings.Contains(string(content), "MEMORY.md") {
		t.Fatal("packaged skill missing", err)
	}
	if strings.Contains(binding.SkillInstructions, "# Restore your context") || strings.Contains(binding.SkillInstructions, "MEMORY.md") {
		t.Fatal("skill body or memory eagerly injected")
	}
	if _, err = os.ReadFile(filepath.Join(filepath.Dir(a.skillPath), "references", "tasks.md")); err != nil {
		t.Fatal(err)
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

func (e *testEngine) SystemReceipt(id string) api.Receipt {
	v := e.Snapshot()
	if v.LastReceipt.ID == id {
		return v.LastReceipt
	}
	return api.Receipt{ID: id, Outcome: "unknown"}
}

func TestRequiredNotebookReadFailureDoesNotBecomeEmptyContext(t *testing.T) {
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	defer a.Close()
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	binding := e.tools.Clone()
	e.mu.Unlock()
	path := filepath.Join(root, "Notebook", "MEMORY.md")
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if seed, err := binding.PrepareContext(t.Context()); err == nil || seed.Text != "" || seed.HandoffDigest != "" {
		t.Fatal("unreadable required memory became a usable empty seed", seed, err)
	}
	if err := os.WriteFile(path, []byte("Corrected identity and authorization limits."), 0600); err != nil {
		t.Fatal(err)
	}
	if seed, err := binding.PrepareContext(t.Context()); err != nil || !strings.Contains(seed.Text, "Corrected identity") {
		t.Fatal("repaired memory unavailable", err)
	}
}
