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

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type switchEngine struct {
	*testEngine
	id      string
	mu      sync.Mutex
	ready   bool
	failure error
}

func (e *switchEngine) ProviderInfo() api.ProviderInfo { return api.ProviderInfo{ID: e.id, Name: e.id} }
func (e *switchEngine) Snapshot() api.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	state := "offline"
	if e.ready {
		state = "ready"
	}
	return api.Snapshot{Connection: state, Phase: "idle"}
}
func (e *switchEngine) Connect(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.testEngine.mu.Lock()
	bound := e.testEngine.tools != nil
	e.testEngine.mu.Unlock()
	if !bound {
		return errors.New("tool binding missing")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failure != nil {
		return e.failure
	}
	e.ready = true
	return nil
}

func newSwitchFixture(t *testing.T, selected bool, targetFailure error) (*Application, string, *[]*switchEngine) {
	t.Helper()
	root := t.TempDir()
	if selected {
		if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"version":1,"runtime":"codex"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	engines := &[]*switchEngine{}
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) {
			e := &switchEngine{testEngine: newTestEngine(), id: id}
			if id == "caelis" {
				e.failure = targetFailure
			}
			*engines = append(*engines, e)
			return e, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, root, engines
}

func TestFirstRuntimeActivationKeepsAppAndService(t *testing.T) {
	a, root, engines := newSwitchFixture(t, false, nil)
	service := a.Backend
	if _, err := a.Backend.SaveDraft(api.Draft{Text: "unsent greeting"}); err != nil {
		t.Fatal(err)
	}
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, true); err != nil {
		t.Fatal(err)
	}
	if a.Backend != service || a.Backend.ProviderInfo().ID != "caelis" || !a.HasRuntimeChoice() || len(*engines) != 2 {
		t.Fatal("app/service changed or first choice not activated in place")
	}
	if draft := a.Backend.Draft(); draft.Text != "unsent greeting" {
		t.Fatalf("unsent text lost: %+v", draft)
	}
	if (*engines)[0].closed != 1 {
		t.Fatal("old idle client not detached")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFailedRuntimeActivationRestoresOriginalClient(t *testing.T) {
	a, root, engines := newSwitchFixture(t, true, errors.New("handshake unavailable"))
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	service := a.Backend
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, false); err == nil {
		t.Fatal("failed handshake committed")
	}
	if a.Backend != service || a.Backend.ProviderInfo().ID != "codex" {
		t.Fatal("old resident not restored")
	}
	if (*engines)[0].closed != 0 || (*engines)[1].closed != 1 {
		t.Fatal("wrong owner detached")
	}
	b, err := os.ReadFile(filepath.Join(root, "runtime.json"))
	if err != nil || !containsRuntime(b, "codex") {
		t.Fatal("active selection changed after failure", err)
	}
}

func TestFailedRuntimeActivationPreservesDraftSavedAfterSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"version":1,"runtime":"codex"}`), 0600); err != nil {
		t.Fatal(err)
	}
	opened, release := make(chan struct{}), make(chan struct{})
	a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) {
			if id == "caelis" {
				close(opened)
				<-release
			}
			e := &switchEngine{testEngine: newTestEngine(), id: id}
			if id == "caelis" {
				e.failure = errors.New("fixture handshake rejected")
			}
			return e, nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	first, err := a.Backend.SaveDraft(api.Draft{Text: "A"})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- a.activateRuntime(context.Background(), api.RuntimeSettings{Runtime: "caelis"}, false)
	}()
	select {
	case <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("switch did not reach target preparation")
	}
	newer, err := a.Backend.SaveDraft(api.Draft{Revision: first.Revision, Text: "B"})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err == nil {
		t.Fatal("failed switch committed")
	}
	if got := a.Backend.Draft(); got.Text != "B" || got.Revision != newer.Revision {
		t.Fatalf("rollback replaced current input: %+v", got)
	}
	reopened := backend.NewService(newTestEngine(), nil, nil, nil, nil)
	if err := reopened.ConfigureDraft(filepath.Join(root, "draft.json")); err != nil {
		t.Fatal(err)
	}
	if got := reopened.Draft(); got.Text != "B" || got.Revision != newer.Revision {
		t.Fatalf("rollback did not persist draft: %+v", got)
	}
}

func TestRuntimeActivationKeepsExistingTaskRoute(t *testing.T) {
	a, root, engines := newSwitchFixture(t, true, nil)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.localWork.routes["already-dispatched"] = "codex"
	service := a.Backend
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, false); err != nil {
		t.Fatal(err)
	}
	if a.Backend != service || a.Backend.ProviderInfo().ID != "caelis" || a.localWork.routes["already-dispatched"] != "codex" || a.localWork.DefaultRuntime() != "codex" {
		t.Fatal("switch changed app, original task route, or existing worker default")
	}
	if (*engines)[0].closed != 1 {
		t.Fatal("old resident client was not detached")
	}
	if saved, err := backend.LoadRuntimeSettings(filepath.Join(root, "runtime.json"), "codex"); err != nil || saved.Runtime != "caelis" {
		t.Fatal("target not committed", saved, err)
	}
}

func TestBotAndNewWorkSelectionsRemainIndependentInAllFourCombinations(t *testing.T) {
	a, _, _ := newSwitchFixture(t, true, nil)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.localWork.routes["original-task"] = "codex"
	assert := func(bot, worker string) {
		t.Helper()
		if got := a.Backend.ProviderInfo().ID; got != bot {
			t.Fatalf("Bot=%s want %s", got, bot)
		}
		if got := a.localWork.DefaultRuntime(); got != worker {
			t.Fatalf("Worker=%s want %s", got, worker)
		}
		if a.localWork.routes["original-task"] != "codex" {
			t.Fatal("original task moved")
		}
	}
	assert("codex", "codex")
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, false); err != nil {
		t.Fatal(err)
	}
	assert("caelis", "codex")
	if err := a.localWork.SelectFirstRuntime("caelis"); err != nil {
		t.Fatal(err)
	}
	assert("caelis", "caelis")
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "codex"}, false); err != nil {
		t.Fatal(err)
	}
	assert("codex", "caelis")
	if err := a.localWork.SelectFirstRuntime("codex"); err != nil {
		t.Fatal(err)
	}
	assert("codex", "codex")
}

func TestCancelledRuntimeActivationRestoresOriginalClient(t *testing.T) {
	a, root, engines := newSwitchFixture(t, true, nil)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.activateRuntime(ctx, api.RuntimeSettings{Runtime: "caelis"}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result = %v", err)
	}
	if a.Backend.ProviderInfo().ID != "codex" || (*engines)[0].Snapshot().Connection != "ready" {
		t.Fatal("cancellation did not restore the original ready client")
	}
	if b, err := os.ReadFile(filepath.Join(root, "runtime.json")); err != nil || !containsRuntime(b, "codex") {
		t.Fatal("cancellation changed the saved selection", err)
	}
}

func TestMissingUserCLILeavesShellHistoryAndSettingsAvailable(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "no-user-cli")
	runtimeFixture, _ := json.Marshal(map[string]any{"version": 1, "runtime": "codex", "cliPath": missing})
	t.Setenv("CODEX_BIN", missing)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	if err := os.WriteFile(filepath.Join(root, "runtime.json"), runtimeFixture, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.Start(); err != nil {
		t.Fatal(err)
	}
	if a.Backend.SetupOverview().Active != "codex" || a.Backend.RuntimeSettings().CLIPath != missing {
		t.Fatal("shell lost selected connection")
	}
	a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "offline-note", Kind: "assistant", Text: "saved locally", Status: "completed"}}})
	if len(a.Backend.ChatSnapshot(0, "").Snapshot.Items) == 0 {
		t.Fatal("local history unavailable without CLI")
	}
	if _, err = os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("test accidentally used a real CLI")
	}
}

func containsRuntime(b []byte, runtime string) bool {
	return len(b) > 0 && string(b) == `{"version":1,"runtime":"`+runtime+`"}`
}

func TestCloseDoesNotInvertSetupAndUpdateLocks(t *testing.T) {
	a, _, _ := newSwitchFixture(t, true, nil)
	a.setup.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- a.Close() }()
	// A setup action already holding its lock must still be able to enter the
	// update transaction while shutdown waits for that action to finish.
	time.Sleep(20 * time.Millisecond)
	acquired := a.updateMu.TryLock()
	if acquired {
		a.updateMu.Unlock()
	}
	a.setup.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not drain setup")
	}
	if !acquired {
		t.Fatal("shutdown held updateMu while waiting for setup")
	}
}
