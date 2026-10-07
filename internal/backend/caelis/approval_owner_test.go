package caelis

import (
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/telegram"
)

func TestApprovalOwnerUsesKnownWorkerBinding(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("snapshot made a native request") })
	s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), HandleId: pointer("h"), RunId: pointer("r"), TurnId: pointer("main-turn")}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Approval: wire.ApprovalState{Active: testApproval()}}, Seen: map[string]bool{}}
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task"}}
	got := s.Snapshot()
	if got.Phase != "working" || got.CurrentTurn == "" || len(got.Approvals) != 1 || got.Approvals[0].Owner != "task" {
		t.Fatalf("known task approval scope: phase=%s owner=%v", got.Phase, got.Approvals)
	}
	if !telegram.TypingForMainTurn(got) {
		t.Fatal("known Caelis Worker approval suppressed the resident turn")
	}
	delete(s.state.Workers, "task")
	got = s.Snapshot()
	if got.Approvals[0].Owner != "" || got.Phase != "waiting_approval" {
		t.Fatalf("unbound session invented independent task authority: phase=%s owner=%s", got.Phase, got.Approvals[0].Owner)
	}
	if telegram.TypingForMainTurn(got) {
		t.Fatal("unbound Caelis session bypassed the approval gate")
	}
	s.state.Views["main"].State.Approval.Active = testApproval()
	got = s.Snapshot()
	var main, unknown bool
	for _, approval := range got.Approvals {
		switch approval.Owner {
		case "conversation":
			main = approval.ID != ""
		case "":
			unknown = true
		default:
			t.Fatalf("unbound session invented task authority: %s", approval.Owner)
		}
	}
	if !main || !unknown || got.Phase != "waiting_approval" {
		t.Fatalf("main/unbound approvals lost their distinct scope: %+v", got.Approvals)
	}
}
