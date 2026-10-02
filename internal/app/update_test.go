package app

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
