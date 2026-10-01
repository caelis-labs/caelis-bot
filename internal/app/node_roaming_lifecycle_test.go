package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/care"
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
