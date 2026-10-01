package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/care"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestNodeRoamingStartRestoresBeforeRetiredSourceAndIsIdempotent(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	a, _ := attachControlRestart(t, f, f.c.options)
	a.sourceRetired = true
	for range 2 {
		if err := a.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if !a.started || a.companion != nil || a.tasks != nil || a.personal != nil || f.staged.Load() != 2 || f.prepared.Load() != 1 {
		t.Fatal("startup did not remain an idempotent observer of the restored owner")
	}
	if _, err := os.Stat(filepath.Join(a.root, "bot.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retired source opened personal state", err)
	}
}

func TestNodeRoamingStartUnknownCrashRemainsCold(t *testing.T) {
	f := roamingControlFixture(t)
	f.stageErr = errors.New("native response lost")
	if state, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil || state.Outcome != "unknown" {
		t.Fatal(state, err)
	}
	o := f.c.options
	reads := 0
	o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
		reads++
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "unknown"}, nil
	}
	a, _ := attachControlRestart(t, f, o)
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err == nil {
		t.Fatal("uncertain persisted ownership admitted original local startup")
	}
	if reads != 1 || a.started || a.personal != nil || a.companion != nil || a.tasks != nil || f.staged.Load() != 1 || f.prepared.Load() != 1 {
		t.Fatal("uncertain startup replayed native work or opened the original source")
	}
}

func TestDefaultNodeRoamingAbsentIntentPreservesLocalStartup(t *testing.T) {
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	if err := AttachNodeManagement(a); err != nil {
		t.Fatal(err)
	}
	if err := attachDefaultNodeRoaming(a); err != nil {
		t.Fatal(err)
	}
	if NodeRoamingOwnsExecution(a) {
		t.Fatal("default local profile acquired roaming authority")
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, e.connectSeen)
	if a.companion == nil || a.tasks == nil || a.personal == nil {
		t.Fatal("default local startup lost its resident services")
	}
	if _, err := os.Stat(filepath.Join(root, "nodeplane", "roaming.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local startup created persistent roaming intent", err)
	}
}

func TestNewRejectsCorruptRoamingIntentBeforeLocalStartup(t *testing.T) {
	for _, contents := range []string{`{"version":1,"phase":"alien"}`, `{"version":1,"phase":"local","extra":true}`, `{"version":1,"phase":"staging","outcome":"unknown"}`} {
		t.Run(contents, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "nodeplane"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "nodeplane", "roaming.json"), []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if a, err := New(root, Host{}); err == nil || a != nil {
				t.Fatal("constructor admitted an invalid persisted authority", a, err)
			}
			if _, err := os.Stat(filepath.Join(root, "bot.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed constructor opened resident identity", err)
			}
		})
	}
}

func TestNodeRoamingNativeActionsUseFreshLocalGeneration(t *testing.T) {
	f := roamingControlFixture(t)
	fresh, freshRoot := fixtureApp(t, newTestEngine(), Host{TrashFile: func(string) error { return nil }})
	f.c.options.RestoreLocal = func(context.Context, NodeRoamingStage) (*Application, error) { return fresh, nil }
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); err != nil {
		t.Fatal(err)
	}
	prefs := fresh.TaskPreferences()
	prefs.MaxRunning = 3
	if saved, err := f.a.SaveTaskPreferences(prefs); err != nil || saved.MaxRunning != 3 || fresh.TaskPreferences().MaxRunning != 3 || f.a.TaskPreferences().MaxRunning != 3 {
		t.Fatal("task preferences did not reach fresh local store", saved, err)
	}
	if f.a.ProviderDirectory() != fresh.ProviderDirectory() || !strings.HasPrefix(f.a.ProviderDirectory(), freshRoot) || f.a.NeedsSetup() != fresh.NeedsSetup() || f.a.HasRuntimeChoice() != fresh.HasRuntimeChoice() {
		t.Fatal("setup actions retained retired source state")
	}
	if err := f.a.PublishCareEvent(t.Context(), care.Event{}); err == nil || err.Error() != "Bot is not running" {
		t.Fatal("care event did not reach the fresh generation", err)
	}
	if err := f.a.PreparePersonal(); err != nil || fresh.personal == nil || f.a.personal != nil {
		t.Fatal("personal data reopened retired source", err)
	}
	for name, call := range map[string]func() error{
		"pin":      func() error { _, err := f.a.PinTask("missing", true); return err },
		"lock":     func() error { _, err := f.a.LockTask("missing", true); return err },
		"clear":    f.a.ClearTasks,
		"move":     func() error { return f.a.MoveTask("missing", "before") },
		"terminal": func() error { _, err := f.a.WorkTerminal(t.Context(), "missing"); return err },
	} {
		if err := call(); err == nil || err.Error() != fresh.text("taskNotConnected") {
			t.Fatalf("%s did not reach the fresh generation: %v", name, err)
		}
	}
	if _, err := f.a.AttachmentStorage(); err != nil {
		t.Fatal("attachment storage did not reach fresh local service", err)
	}
	if _, err := f.a.CleanAttachments(t.Context()); err != nil {
		t.Fatal("attachment cleanup did not reach fresh local service", err)
	}
	if err := f.a.PrepareRestart(); err != nil {
		t.Fatal("restart did not reach fresh local service", err)
	}
	if err := fresh.PrepareRestart(); err == nil {
		t.Fatal("restart failed to freeze admission on the fresh local service")
	}
	fresh.Backend.CancelRestart()
}

func TestNativeCoordinatorGuardRejectsBeforeCatalogOrDocumentMutation(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	// No catalog or writer is installed: the guard must reject before either
	// is consulted, including direct native calls that bypass Backend settings.
	n := &nativeNodeManagement{app: f.a}
	if _, err := n.SetCoordinator(t.Context(), api.NodeCoordinatorSelection{NodeID: "different"}); err == nil || !strings.Contains(err.Error(), "disable automatic roaming") {
		t.Fatal("direct native coordinator mutation escaped the guard", err)
	}
}

func TestNodeRoamingStableBackendUsesFreshSetupAndTasksAfterDisableAndRestart(t *testing.T) {
	f := roamingControlFixture(t)
	freshEngine := newTestEngine()
	fresh, freshRoot := fixtureApp(t, freshEngine, Host{})
	f.c.options.RestoreLocal = func(context.Context, NodeRoamingStage) (*Application, error) { return fresh, nil }
	f.c.startLocal = func(_ context.Context, a *Application) error { return a.Start() }
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Backend.SaveProductPairing(backend.ProductPairing{Mode: "local"}, f.a.Backend.ProductConnection().Revision); err == nil {
		t.Fatal("pairing bypassed enabled roaming authority")
	}
	if _, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, freshEngine.connectSeen)
	task, err := fresh.tasks.StartTask(t.Context(), api.TaskStart{RequestID: "fresh-work", Title: "Fresh task", Prompt: "Fixture work"})
	if err == nil || task.ID == "" || task.Status != "unknown" {
		t.Fatal("fixture did not retain its uncertain fresh work", task, err)
	}
	assertFresh := func(a, owner *Application) {
		t.Helper()
		if rows := a.Backend.TaskSummaries(); len(rows) != 1 || rows[0].ID != task.ID {
			t.Fatal("stable renderer task port did not follow fresh work", rows)
		}
		guardErr := owner.guardRuntimeChange()
		if err := a.Backend.ActivateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}); guardErr == nil || err == nil || err.Error() != guardErr.Error() {
			t.Fatal("renderer activation bypassed actual fresh work", guardErr, err)
		}
		if a.Backend.SetupOverview().Active != "fixture" {
			t.Fatal("renderer setup retained the original source")
		}
		state := a.Backend.ProductConnection()
		if state.ActiveMode != "local" || state.Pairing.Mode != "local" {
			t.Fatal("disabled local generation projected remote pairing", state)
		}
		if port := backend.NativeProductConnectionController(a.Backend); port != backend.NativeProductConnectionController(owner.Backend) {
			if _, ok := port.(*nodeRoamingControl); !ok {
				t.Fatal("outer roaming controller was replaced")
			}
		}
		if _, err := a.Backend.SaveProductPairing(backend.ProductPairing{Mode: "local"}, state.Revision); err != nil {
			t.Fatal("disabled pairing did not delegate to fresh CAS authority", err)
		}
		if err := a.Backend.DismissSetup(); err != nil {
			t.Fatal(err)
		}
	}
	assertFresh(f.a, fresh)
	for _, name := range []string{"setup.json", "product-connection.json"} {
		if _, err := os.Stat(filepath.Join(freshRoot, name)); err != nil {
			t.Fatal("renderer action missed the fresh generation", name, err)
		}
		if _, err := os.Stat(filepath.Join(f.a.root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("renderer action mutated the retired source", name, err)
		}
	}
	if err := f.c.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newApplication(freshRoot, Host{}, func(id string) (providerFactory, error) {
		return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) { return newTestEngine(), nil }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	o := f.c.options
	o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "accepted", Phase: "local", Local: reopened}, nil
	}
	a, _ := attachControlRestart(t, f, o)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	assertFresh(a, reopened)
}

func TestNodeRoamingUnknownPairingCannotReachLocalController(t *testing.T) {
	f := roamingControlFixture(t)
	f.stageErr = errors.New("native response lost")
	if state, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil || state.Outcome != "unknown" {
		t.Fatal(state, err)
	}
	state := f.a.Backend.ProductConnection()
	if state.ActiveMode != "remote" || state.Pairing.Mode != "remote" {
		t.Fatal("unknown roaming authority projected local mode", state)
	}
	if _, err := f.a.Backend.SaveProductPairing(backend.ProductPairing{Mode: "local"}, state.Revision); err == nil {
		t.Fatal("pairing bypassed unknown native authority")
	}
}

func TestNodeRoamingPreparingReenableRecoveryKeepsVerifiedFreshSource(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); err != nil {
		t.Fatal(err)
	}
	freshRoot := f.c.local.root
	current := api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/current-choice"}
	if err := localstate.Write(filepath.Join(freshRoot, "runtime.json"), current); err != nil {
		t.Fatal(err)
	}
	f.preflightErr = errors.New("process disappeared during native preparation")
	if state, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-next-original")); err == nil || state.Outcome != "unknown" || f.c.doc.Phase != "preparing" || f.c.doc.SourceRetiredIntent {
		t.Fatal("fixture did not stop before retiring the fresh source", state, err)
	}
	if err := f.c.Close(); err != nil {
		t.Fatal(err)
	}
	o := f.c.options
	recoveries, starts := 0, 0
	o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
		recoveries++
		if in.OperationID != "enable-next-original" || in.StageOperationID != "enable-next-original" || in.OperationKind != "enable" || in.SourceRetiredIntent || in.LocalGenerationDirectory != freshRoot || (in.Phase != "preparing" && in.Phase != "local") {
			t.Fatal("recovery changed the original request or source generation", in)
		}
		settings, err := backend.LoadRuntimeSettings(filepath.Join(freshRoot, "runtime.json"), "codex")
		if err != nil || settings != current {
			t.Fatal("recovery replaced the current source preferences", settings, err)
		}
		engine := &controlLocalEngine{}
		local := &Application{root: freshRoot, engine: engine, Backend: backend.NewService(engine, func([]string) ([]api.InputFile, error) { return nil, nil }, func([]string) {}, nil, nil)}
		local.Backend.ConfigureRuntime(filepath.Join(freshRoot, "runtime.json"), settings)
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "rejected", Phase: "source-active", Local: local}, nil
	}
	for range 2 {
		a, c := attachControlRestart(t, f, o)
		a.sourceRetired = true
		c.startLocal = func(_ context.Context, local *Application) error {
			starts++
			saved, err := loadNodeRoamingDocument(c.filename())
			if err != nil || saved.Enabled || saved.Phase != "local" || saved.Outcome != "rejected" || saved.OperationID != "enable-next-original" || saved.LocalGenerationDirectory != freshRoot {
				t.Fatal("source admission opened before durable original rejection", saved, err)
			}
			local.mu.Lock()
			local.started = true
			local.mu.Unlock()
			return nil
		}
		for range 2 {
			if err := a.Start(); err != nil {
				t.Fatal(err)
			}
		}
		if c.local == nil || c.local.root != freshRoot || c.proxy.Current() != c.local.engine || c.proxy.Current() == a.engine || a.personal != nil || a.tasks != nil || a.companion != nil {
			t.Fatal("preparing re-enable recovery reopened the retired original APP")
		}
		if a.Backend.RuntimeSettings() != current || c.doc.OperationID != "enable-next-original" || c.doc.StageOperationID != "enable-next-original" || c.doc.Outcome != "rejected" || c.state.Enabled || c.state.ActiveBotNodeID != api.LocalNodeID {
			t.Fatal("verified source did not retain its current configuration and original receipt")
		}
		if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "fresh-submit", Text: "fixture"}); err != nil || a.engine.(*controlLocalEngine).submits.Load() != 0 || c.local.engine.(*controlLocalEngine).submits.Load() != 1 {
			t.Fatal("recovered source did not own new submissions", err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if recoveries != 2 || starts != 2 || f.prepared.Load() != 1 || f.staged.Load() != 1 || f.disabled.Load() != 1 {
		t.Fatal("recovery replayed native preparation, enable, disable, or startup", recoveries, starts)
	}
}

func TestNodeRoamingPreparingReenableRejectsUnverifiedFreshSource(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(map[bool]string{true: "missing", false: "different-generation"}[missing], func(t *testing.T) {
			f := roamingControlFixture(t)
			if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); err != nil {
				t.Fatal(err)
			}
			f.preflightErr = errors.New("preparation response lost")
			if state, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-next-original")); err == nil || state.Outcome != "unknown" {
				t.Fatal(state, err)
			}
			o := f.c.options
			o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
				result := NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "rejected", Phase: "source-active"}
				if !missing {
					engine := &controlLocalEngine{}
					result.Local = &Application{root: t.TempDir(), engine: engine, Backend: backend.NewService(engine, nil, nil, nil, nil)}
				}
				return result, nil
			}
			a, c := attachControlRestart(t, f, o)
			if err := a.Start(); err == nil {
				t.Fatal("unverified generation admitted original local startup")
			}
			if a.started || a.personal != nil || a.tasks != nil || c.local != nil || c.proxy.Current() == a.engine || c.doc.Outcome != "unknown" || f.prepared.Load() != 1 || f.staged.Load() != 1 {
				t.Fatal("failed recovery reopened or replayed the original source")
			}
		})
	}
}

func TestNodeRoamingUndispatchedDisableRecoveryKeepsOriginalActiveAuthority(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-next")); err != nil {
		t.Fatal(err)
	}
	// Model the exact crash after syncing the original disable intent, before
	// its native stage.Disable callback can dispatch a stop or change authority.
	f.c.doc.OperationID = "disable-next-original"
	f.c.doc.OperationKind = "disable"
	f.c.doc.Phase = "quiescing"
	f.c.doc.Outcome = "unknown"
	if err := f.c.save(); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Close(); err != nil {
		t.Fatal(err)
	}
	o := f.c.options
	recoveries := 0
	o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
		recoveries++
		if in.OperationID != "disable-next-original" || in.StageOperationID != "enable-next" || in.OperationKind != "disable" || (in.Phase != "quiescing" && in.Phase != "active") || !in.SourceRetiredIntent || in.LocalGenerationDirectory == "" || in.StageInput.OperationID != "enable-next" {
			t.Fatal("recovery changed the original disable or active enable authority", in)
		}
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "rejected", Phase: "active", Stage: NodeRoamingStage{
			Broker: f.broker,
			Disable: func(context.Context, string) error {
				t.Fatal("recovery replayed native disable")
				return nil
			},
			Close: func() error { return nil },
		}}, nil
	}
	for restart := range 2 {
		a, c := attachControlRestart(t, f, o)
		a.sourceRetired = true
		for range 2 {
			if err := a.Start(); err != nil {
				t.Fatal(err)
			}
		}
		if c.doc.OperationID != "disable-next-original" || c.doc.StageOperationID != "enable-next" || c.doc.Outcome != "rejected" || c.doc.Phase != "active" || !c.doc.Enabled || !c.state.Enabled || c.state.ActiveBotNodeID != "node-fixture" {
			t.Fatal("recovery changed the original rejected disable or active owner", c.doc, c.state)
		}
		if a.personal != nil || a.tasks != nil || a.companion != nil || c.proxy.Current() == a.engine || c.local != nil {
			t.Fatal("rejected disable reopened a local source")
		}
		if f.prepared.Load() != 2 || f.disabled.Load() != 1 || f.staged.Load() != 2 || recoveries != restart+1 {
			t.Fatal("recovery replayed native authority instead of reconnecting", restart, f.prepared.Load(), f.disabled.Load(), f.staged.Load(), recoveries)
		}
		if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "active-owner", Text: "fixture"}); err != nil || a.engine.(*controlLocalEngine).submits.Load() != 0 {
			t.Fatal("recovered active authority did not own the product command", err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNodeRoamingRejectedDisableUnknownRecoveryNeverResumes(t *testing.T) {
	f := roamingControlFixture(t)
	if _, err := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); err != nil {
		t.Fatal(err)
	}
	f.c.doc.OperationID = "disable-original"
	f.c.doc.OperationKind = "disable"
	f.c.doc.Outcome = "rejected"
	if err := f.c.save(); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Close(); err != nil {
		t.Fatal(err)
	}
	o := f.c.options
	o.Recover = func(_ context.Context, in NodeRoamingRecoveryInput) (NodeRoamingRecovery, error) {
		if in.OperationID != "disable-original" || in.StageOperationID != "enable-original" || in.Phase != "active" {
			t.Fatal("recovery changed original authority", in)
		}
		return NodeRoamingRecovery{OperationID: in.OperationID, Outcome: "unknown"}, nil
	}
	a, c := attachControlRestart(t, f, o)
	if err := a.Start(); err == nil {
		t.Fatal("unknown recovery resumed a previously rejected disable")
	}
	if a.started || a.personal != nil || a.tasks != nil || c.local != nil || c.proxy.Current() == a.engine || f.staged.Load() != 1 {
		t.Fatal("unknown recovery replayed the old stage or reopened the original source")
	}
}
