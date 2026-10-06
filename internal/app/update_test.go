package app

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type updateEngine struct {
	*testEngine
	snapshot api.Snapshot
	detached int
}

func (e *updateEngine) Snapshot() api.Snapshot                { return e.snapshot }
func (e *updateEngine) DetachForUpdate(context.Context) error { e.detached++; return nil }

type legacyUpdateEngine struct {
	*testEngine
	snapshot api.Snapshot
}

func (e *legacyUpdateEngine) Snapshot() api.Snapshot { return e.snapshot }

func TestUpdateAdmitsUnresolvedConversationAndFreezesAdmission(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	for _, snapshot := range []api.Snapshot{
		{CanInterrupt: true}, {Phase: "unknown"}, {Phase: "sending"},
		{Approvals: []api.Approval{{Status: "pending"}}},
		{Approvals: []api.Approval{{Status: "resolved"}}},
	} {
		e.snapshot = snapshot
		if err := a.PrepareUpdate(); err != nil {
			t.Fatal("explicit update was blocked by conversation state", snapshot, err)
		}
		if e.closed != 0 {
			t.Fatal("update cancelled backend work")
		}
		if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "must-not-run"}); err == nil {
			t.Fatal("admission remained open")
		}
		a.CancelUpdate()
	}
	e.snapshot = api.Snapshot{}
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "must-not-run"}); err == nil {
		t.Fatal("admission remained open")
	}
	a.CancelUpdate()
}

func TestUpdateCloseDetachesWithoutInterruptingOriginalOwner(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanInterrupt: true, Approvals: []api.Approval{{Status: "pending"}}}}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if e.detached != 1 || e.closed != 0 {
		t.Fatal("update used ordinary close instead of detach", e.detached, e.closed)
	}
}

func TestUpdateFailsBeforeClosingActiveLegacyOwner(t *testing.T) {
	e := &legacyUpdateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanInterrupt: true}}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.PrepareUpdate(); err == nil {
		t.Fatal("active owner without a safe detach was admitted")
	}
	if e.closed != 0 {
		t.Fatal("active owner was closed")
	}
	e.snapshot = api.Snapshot{}
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal("failed preparation left admission frozen", err)
	}
	a.CancelUpdate()
}

func TestResolvedApprovalDoesNotBlockConversationChange(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Approvals: []api.Approval{{Status: "resolved"}}}}
	a, _ := fixtureApp(t, e, Host{})
	if err := a.guardConversationChange(); err != nil {
		t.Fatal("resolved approval was treated as pending", err)
	}
	e.snapshot.Approvals = []api.Approval{{Status: "pending"}}
	if err := a.guardConversationChange(); err == nil {
		t.Fatal("pending approval was ignored")
	}
}
