package caelis

import (
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Native Worker bindings intentionally have no private profile. The public
// snapshot owns caller-resolvability; review delivery must not create it.
func TestNativeWorkerApprovalSnapshotAndReviewOrdering(t *testing.T) {
	for _, terminal := range []string{"approved", "denied", "failed", "timed_out"} {
		t.Run(terminal, func(t *testing.T) {
			s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("projection dispatched a request") })
			s.state.Workers["task"] = worker{Native: true, Binding: wire.SessionBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "working"}}
			state := wire.SessionState{SessionId: "work", Run: wire.RunState{Active: pointer(true), WaitingApproval: pointer(true), TurnId: pointer("turn")}}
			v := &view{State: state}
			s.state.Views["work"] = v
			assertNoManual := func() {
				t.Helper()
				if len(s.Snapshot().Approvals) != 0 || !value(v.State.Run.WaitingApproval) {
					t.Fatal("automatic waiting work became a manual decision or lost activity")
				}
			}
			assertNoManual() // Bootstrap before the first review event.
			v = &view{State: state}
			s.state.Views["work"] = v
			assertNoManual() // Reconnect with no local review history.
			e := wire.Envelope{Kind: "caelis/approval_review", SessionId: pointer("work"), TurnId: pointer("turn"), ApprovalRequestId: pointer("automatic"), Delivery: wire.Delivery{Mode: wire.DeliveryModeMirror}, ApprovalReview: &wire.ApprovalReview{ToolCallId: pointer("reused-provider-call"), Status: pointer(terminal)}}
			applyEnvelope(v, e) // Terminal arrives before progress.
			applyEnvelope(v, e) // Duplicate terminal replay.
			e.ApprovalReview.Status = pointer("in_progress")
			for range 2 {
				applyEnvelope(v, e)
				assertNoManual()
			}
			want := terminal
			if want == "timed_out" {
				want = "timedOut"
			}
			if got := s.Snapshot().Reviews; len(got) != 1 || got[0].Status != want {
				t.Fatal("delayed progress regressed the original terminal review", got)
			}
			// A real native manual request remains visible even with review
			// history and a reused provider call ID. It retains native choices.
			a := testApproval()
			a.RequestId = "manual"
			a.Target.TurnId = "turn"
			v.State.Approval.Active = a
			applyEnvelope(v, e)
			got := s.Snapshot().Approvals
			if len(got) != 1 || got[0].NativeID != "manual" || got[0].TaskID != "task" || got[0].Owner != "task" {
				t.Fatal("review history hid or retargeted a real manual request", got)
			}
			v.State.Approval.Active = nil
			if len(s.Snapshot().Approvals) != 0 {
				t.Fatal("settled native manual request resurrected")
			}
		})
	}
}
