package app

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestManagedUpdateWaitsForOriginalWorkAndUnknown(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	for _, snapshot := range []api.Snapshot{
		{CanInterrupt: true}, {Phase: "sending"}, {Phase: "unknown"},
		{LastReceipt: api.Receipt{ID: "original", Outcome: "unknown"}},
		{Approvals: []api.Approval{{ID: "original-decision", Status: "pending"}}},
	} {
		e.snapshot = snapshot
		if err := a.PrepareIdleUpdate(); err == nil {
			t.Fatal("active/unknown owner admitted", snapshot)
		}
		if a.updatePrepared || e.detached != 0 || e.closed != 0 {
			t.Fatal("failed preflight changed owner")
		}
	}
	e.snapshot = api.Snapshot{}
	if err := a.PrepareIdleUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "racing-input"}); err == nil {
		t.Fatal("admission not fenced")
	}
	a.CancelUpdate()
	if a.updatePrepared {
		t.Fatal("cancel retained fence")
	}
}
