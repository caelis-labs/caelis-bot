package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// This uses the App Server wire entry and the real session projection. The
// Telegram gate receives the resulting snapshot shape in typing_test.go.
func TestNativeResolvedApprovalRetainedWhileMainTurnContinues(t *testing.T) {
	s, f := sessionPair(t, "hold")
	if receipt := sendSynthetic(t, s, "typing-main"); receipt.Outcome != "accepted" {
		t.Fatalf("native submission outcome = %s", receipt.Outcome)
	}
	before := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "ready" && v.Phase == "working" && v.CurrentTurn != "" && len(v.Approvals) == 0
	})
	f.emit(approvalMessage("typing-native-approval"))
	pending := awaitState(t, s, func(v api.Snapshot) bool {
		return len(v.Approvals) == 1 && v.Approvals[0].Status == "pending"
	})
	if pending.CurrentTurn != before.CurrentTurn || len(pending.Approvals[0].Choices) == 0 {
		t.Fatalf("native pending approval lost the main turn or offered choices: beforeTurn=%t pendingTurn=%t phase=%s choices=%d", before.CurrentTurn != "", pending.CurrentTurn != "", pending.Phase, len(pending.Approvals[0].Choices))
	}
	if err := s.Decide(testContext(t), api.Decision{ID: pending.Approvals[0].ID, Choice: pending.Approvals[0].Choices[0].ID}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"typing-native-approval"` || string(answer.Result) != `{"decision":"accept"}` {
		t.Fatal("fixture did not receive the offered native decision")
	}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": "thread-native", "requestId": "typing-native-approval"})})
	resolved := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "working" && v.CurrentTurn == before.CurrentTurn && len(v.Approvals) == 1 && v.Approvals[0].Status == "resolved"
	})
	if !resolved.CanSteer || resolved.LoginPending {
		t.Fatal("resolved main turn did not resume normal work authority")
	}
}
