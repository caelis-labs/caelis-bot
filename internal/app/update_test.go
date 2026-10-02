package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
	"path/filepath"
	"testing"
)

type updateEngine struct {
	*testEngine
	snapshot api.Snapshot
}

func (e *updateEngine) Snapshot() api.Snapshot { return e.snapshot }

func TestUpdateRejectsBusyAndUnknownThenFreezesAdmission(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	for _, snapshot := range []api.Snapshot{
		{CanInterrupt: true}, {Phase: "unknown"}, {Phase: "sending"},
		{Approvals: []api.Approval{{}}},
	} {
		e.snapshot = snapshot
		if err := a.PrepareUpdate(); err == nil {
			t.Fatal("update admitted during unresolved work", snapshot)
		}
		if e.closed != 0 {
			t.Fatal("update cancelled backend work")
		}
	}
	e.snapshot = api.Snapshot{}
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "must-not-run"}); err == nil {
		t.Fatal("admission remained open")
	}
	a.CancelUpdate()
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal("cancel did not release admission", err)
	}
}

func TestNotebookBusyStopRejectionDoesNotPersistRetirement(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanInterrupt: true}}
	a, _ := fixtureApp(t, e, Host{})
	a.started = true
	hooks, _ := a.NotebookLocalSourceHooks()
	hooks.StandbyStopped = func(context.Context, string) error { return nil }
	hooks.Transfer = func(context.Context, string, bool) error { return nil }
	hooks.StartFresh = func(context.Context, string) error { return nil }
	hooks.Save = func(s notebooksync.State) error {
		return localstate.Write(filepath.Join(a.root, "nodeplane", "notebook-sync.json"), s)
	}
	c, err := notebooksync.New(notebooksync.State{SourceNodeID: "local", Targets: []notebooksync.Status{{NodeID: "backup", Phase: "ready"}}}, hooks)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Switch(t.Context(), "backup"); !errors.Is(err, notebooksync.ErrStopNotDispatched) {
		t.Fatal(err)
	}
	if e.closed != 0 || a.sourceRetired || c.State().Targets[0].Phase != "ready" {
		t.Fatal("busy rejection retired owner")
	}
	e.snapshot = api.Snapshot{}
	if err = a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	a.CancelUpdate()
	restarted := notebookSettingsApp(t)
	restarted.root = a.root
	if err = attachDefaultNotebookSync(restarted); err != nil {
		t.Fatal(err)
	}
	if err = restarted.notebookSyncStartupGuard(); err != nil {
		t.Fatal("busy rejection blocked restart", err)
	}
}
