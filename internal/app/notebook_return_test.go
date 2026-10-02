package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func returnFixture(t *testing.T) (*defaultNotebookSync, string, *bool) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := NodeRegistration{ID: "remote", Label: "Remote", Join: api.NodeSSH, SSHDestination: "fixture", Directory: remote, HelperPath: filepath.Join(remote, "helper")}
	write := func(name string, v any) {
		t.Helper()
		if err := localstate.Write(filepath.Join(root, name), v); err != nil {
			t.Fatal(err)
		}
	}
	if err := backend.SaveRuntimeSettingsDocument(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/codex"}); err != nil {
		t.Fatal(err)
	}
	write("bot.json", bot.State{Version: 1, PersonalVersion: 1, ID: "fixture-bot", Schedules: []bot.Schedule{}})
	write("nodeplane/config.json", nodeManagementDocument{Version: 1, Revision: 1, Nodes: []NodeRegistration{r}})
	write("nodeplane/notebook-local-owner.json", notebookLocalStop{BotID: "fixture-bot", Stopped: true})
	write("conversation.json", map[string]string{"session": "old-local-binding"})
	write("personal/old.json", map[string]string{"evidence": "old-native-evidence"})
	for _, p := range []string{filepath.Join(root, "Notebook"), filepath.Join(nodeagent.NotebookOwnerProfile(remote), "Notebook")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Notebook", "old-only.md"), []byte("preserve old local note"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeagent.NotebookOwnerProfile(remote), "Notebook", "MEMORY.md"), []byte("current remote notes"), 0600); err != nil {
		t.Fatal(err)
	}
	pairing := backend.ProductPairing{Mode: "remote", NodeID: r.ID, BotID: productrpc.ProfileBotID("fixture-bot"), Endpoint: "http://127.0.0.1:1"}
	a := &Application{root: root, Backend: backend.NewService(nil, nil, nil, nil, nil), product: &productEngine{pairing: pairing}}
	a.Backend.ConfigureProductConnection(newProductPairingController(root, pairing, nil))
	c := &defaultNotebookSync{app: a, settings: backend.NotebookSyncSettings{Enabled: true, SourceNodeID: r.ID, SourceBackend: api.NodeCaelis, IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{{NodeID: "local", Backend: api.NodeCodex}}}}
	stopped := false
	c.ownerCall = func(_ context.Context, _ NodeRegistration, in nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
		if in.Action == "stop" {
			stopped = true
		}
		return nodeagent.NotebookOwnerState{Stopped: stopped, Endpoint: pairing.Endpoint, Identity: productrpc.Identity{NodeID: r.ID, Scope: productrpc.Scope{BotID: pairing.BotID}}}, nil
	}
	write("nodeplane/notebook-settings.json", c.settings)
	return c, nodeagent.NotebookOwnerProfile(remote), &stopped
}
func TestNotebookReturnPreservesOldProfileAndWaitsForOrdinaryRuntimeReady(t *testing.T) {
	c, remote, stopped := returnFixture(t)
	o, err := c.options(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	h := o.Hooks
	h.Transfer = func(ctx context.Context, id string, final bool) error {
		if !*stopped {
			t.Fatal("copy before remote stop")
		}
		if err := o.BeforeFinalTransfer(ctx, id); err != nil {
			return err
		}
		return (notebooksync.Rsync{}).Sync(ctx, notebooksync.Endpoint{Profile: remote}, notebooksync.Endpoint{Profile: c.app.root}, "return-fixture", final, nil)
	}
	h.Save = func(s notebooksync.State) error {
		return localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-sync.json"), s)
	}
	controller, err := notebooksync.New(notebooksync.State{SourceNodeID: "remote", Targets: []notebooksync.Status{{NodeID: "local", Phase: "ready"}}}, h)
	if err != nil {
		t.Fatal(err)
	}
	c.app.notebookSync = controller
	if err := c.Switch(t.Context(), "local"); err != nil {
		t.Fatal(err)
	}
	s := controller.State().Targets[0]
	if s.Phase != "restart-required" || c.settings.SourceNodeID != "remote" {
		t.Fatal("return falsely completed before normal local Runtime", s, c.settings)
	}
	for _, p := range []string{"Product/notebook-before-return-" + s.OperationID + "/Notebook/old-only.md", "Product/retired-" + s.OperationID + "/conversation.json", "Product/retired-" + s.OperationID + "/personal/old.json"} {
		if _, err := os.Stat(filepath.Join(c.app.root, p)); err != nil {
			t.Fatal(p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(c.app.root, "conversation.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old local binding resumed")
	}
	if b, err := os.ReadFile(filepath.Join(c.app.root, "Notebook", "MEMORY.md")); err != nil || string(b) != "current remote notes" {
		t.Fatal(string(b), err)
	}
	if err := c.app.PrepareRestart(); err != nil {
		t.Fatal("prepared switch cannot use native relaunch", err)
	}
	pairing, err := loadProductPairing(c.app.root)
	if err != nil || pairing.Mode != "local" {
		t.Fatal(pairing, err)
	}
	reopened := &Application{root: c.app.root, Backend: backend.NewService(nil, nil, nil, nil, nil)}
	if err := attachDefaultNotebookSync(reopened); err != nil {
		t.Fatal(err)
	}
	pending := reopened.notebookReturn
	if pending == nil {
		t.Fatal("normal local return admission absent")
	}
	pending.ownerCall = c.ownerCall
	*stopped = false
	if err := reopened.notebookSyncStartupGuard(); err == nil {
		t.Fatal("live remote owner permitted second local owner")
	}
	*stopped = true
	originalCall := pending.ownerCall
	pending.ownerCall = func(context.Context, NodeRegistration, nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
		return nodeagent.NotebookOwnerState{}, errors.New("SSH unavailable")
	}
	if err := reopened.notebookSyncStartupGuard(); err == nil {
		t.Fatal("SSH loss treated as stopped proof")
	}
	pending.ownerCall = originalCall
	if err := reopened.notebookSyncStartupGuard(); err != nil {
		t.Fatal(err)
	}
	reopened.started = true
	if err := pending.observeReturn(t.Context(), api.Snapshot{Connection: "connecting"}); err != nil || pending.returning == nil {
		t.Fatal("connecting completed return", err)
	}
	if err := pending.observeReturn(t.Context(), api.Snapshot{Connection: "ready"}); err != nil {
		t.Fatal(err)
	}
	if reopened.notebookReturn != nil {
		t.Fatal("completed return kept startup validator")
	}
	// The actual save path holds c.mu and enters PreparePersonal/startup guard.
	// A contained stopped APP makes preparation return before opening native stores.
	reopened.mu.Lock()
	reopened.closed = true
	reopened.mu.Unlock()
	completedSave := make(chan struct{})
	go func() {
		_, _ = pending.SaveSettings(context.Background(), backend.NotebookSyncSettings{Enabled: true, IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{{NodeID: "remote", Backend: api.NodeCaelis}}})
		close(completedSave)
	}()
	select {
	case <-completedSave:
	case <-time.After(2 * time.Second):
		t.Fatal("save after completed return deadlocked")
	}
	t.Cleanup(func() { reopened.closeNotebookSync(); reopened.workers.Wait() })
	prefs := pending.Settings()
	if prefs.SourceNodeID != "local" || prefs.SourceBackend != api.NodeCodex || len(prefs.Targets) != 1 || prefs.Targets[0].NodeID != "remote" || prefs.Targets[0].Backend != api.NodeCaelis {
		t.Fatal("backup source did not follow normal local owner", prefs)
	}
	if pending.returning != nil || reopened.notebookSync.State().SourceNodeID != "local" {
		t.Fatal("ready local owner still pending")
	}
}
func TestNotebookConfirmedRemoteMoveAutomaticallyReversesBackupOnReopen(t *testing.T) {
	c, _, _ := returnFixture(t)
	c.settings = backend.NotebookSyncSettings{Enabled: true, SourceNodeID: "local", SourceBackend: api.NodeCodex, IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{{NodeID: "remote", Backend: api.NodeCaelis}}}
	c.state = notebooksync.State{SourceNodeID: "local", Targets: []notebooksync.Status{{NodeID: "remote", Phase: "switched", OperationID: "confirmed"}}}
	if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-settings.json"), c.settings); err != nil {
		t.Fatal(err)
	}
	if err := localstate.Write(filepath.Join(c.app.root, "nodeplane", "notebook-sync.json"), c.state); err != nil {
		t.Fatal(err)
	}
	if err := attachDefaultNotebookSync(c.app); err != nil {
		t.Fatal(err)
	}
	prefs, err := c.app.Backend.NotebookSyncSettings()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.SourceNodeID != "remote" || prefs.SourceBackend != api.NodeCaelis || prefs.Targets[0].NodeID != "local" || prefs.Targets[0].Backend != api.NodeCodex {
		t.Fatal(prefs)
	}
	if c.app.notebookSync == nil || c.app.notebookSync.State().SourceNodeID != "remote" || c.app.notebookSyncInterval != 5*time.Minute {
		t.Fatal("automatic remote source assembly missing")
	}
	c.app.closeNotebookSync()
}
func TestNotebookReturnStopFailureKeepsLocalNotebookAndBindingUntouched(t *testing.T) {
	c, _, _ := returnFixture(t)
	o, err := c.options(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	original := c.ownerCall
	c.ownerCall = func(ctx context.Context, r NodeRegistration, in nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
		if in.Action == "stop" {
			return nodeagent.NotebookOwnerState{}, errors.New("stop response lost")
		}
		return original(ctx, r, in)
	}
	h := o.Hooks
	h.Transfer = func(context.Context, string, bool) error { t.Fatal("copied after uncertain stop"); return nil }
	h.Save = func(notebooksync.State) error { return nil }
	controller, err := notebooksync.New(notebooksync.State{SourceNodeID: "remote", Targets: []notebooksync.Status{{NodeID: "local"}}}, h)
	if err != nil {
		t.Fatal(err)
	}
	c.app.notebookSync = controller
	if err := c.Switch(t.Context(), "local"); err == nil {
		t.Fatal("uncertain stop admitted")
	}
	for _, p := range []string{"Notebook/old-only.md", "conversation.json"} {
		if _, err := os.Stat(filepath.Join(c.app.root, p)); err != nil {
			t.Fatal("original changed", p, err)
		}
	}
	if c.app.notebookRestartPrepared {
		t.Fatal("uncertain stop admitted restart")
	}
}

func TestNotebookReturnCompletesThroughNormalApplicationSnapshotObserver(t *testing.T) {
	engine := newTestEngine()
	ready := make(chan struct{})
	a, root := fixtureApp(t, engine, Host{Observe: func(s api.Snapshot) {
		if s.Connection == "ready" {
			close(ready)
		}
	}})
	write := func(name string, v any) {
		t.Helper()
		if err := localstate.Write(filepath.Join(root, name), v); err != nil {
			t.Fatal(err)
		}
	}
	write("bot.json", bot.State{Version: 1, PersonalVersion: 1, ID: "fresh-local-bot", Schedules: []bot.Schedule{}})
	remote := NodeRegistration{ID: "remote", Label: "Remote", Join: api.NodeSSH, SSHDestination: "fixture", Directory: "/fixture/remote", HelperPath: "/fixture/remote/helper"}
	write("nodeplane/config.json", nodeManagementDocument{Version: 1, Revision: 1, Nodes: []NodeRegistration{remote}})
	c := &defaultNotebookSync{app: a, settings: backend.NotebookSyncSettings{Enabled: true, SourceNodeID: "remote", SourceBackend: api.NodeCaelis, IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{{NodeID: "local", Backend: api.NodeCodex}}}, returning: &notebookLocalReturn{OperationID: "prepared", SourceNodeID: "remote", BotID: "fresh-local-bot"}}
	c.ownerCall = func(context.Context, NodeRegistration, nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
		return nodeagent.NotebookOwnerState{Stopped: true}, nil
	}
	a.notebookReturn = c
	a.Backend.ConfigureNotebookSync(c)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, engine.connectSeen)
	if a.notebookSync != nil {
		t.Fatal("return completed before actual snapshot")
	}
	engine.snapshots <- api.Snapshot{Revision: 1, Connection: "ready"}
	waitSignal(t, ready)
	prefs := c.Settings()
	if prefs.SourceNodeID != "local" || a.notebookSync == nil || a.notebookSync.State().SourceNodeID != "local" {
		t.Fatal("normal Runtime observer did not finish return", prefs)
	}
}
