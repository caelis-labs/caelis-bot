package codex

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/telegram"
)

func TestNativeWorkerApprovalSnapshotKeepsTelegramMainTyping(t *testing.T) {
	s, f := sessionPair(t, "hold")
	if receipt := sendSynthetic(t, s, "typing-main-with-worker"); receipt.Outcome != "accepted" {
		t.Fatal(receipt.Outcome)
	}
	s.mu.Lock()
	s.rememberChild("independent-worker")
	s.binding.Tasks = map[string]*taskRecord{"task": {Thread: "independent-worker", View: api.Task{Status: "working", Title: "synthetic task"}}}
	s.childRuns["independent-worker"] = "worker-turn"
	s.update()
	s.mu.Unlock()
	f.emit(wireMessage{ID: raw("worker-approval"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
		"threadId": "independent-worker", "turnId": "worker-turn", "itemId": "worker-command",
		"command": "synthetic", "availableDecisions": []string{"accept", "decline"},
	})})
	worker := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	if worker.Phase != "working" || !worker.CanSteer || worker.Approvals[0].Owner != "task" || worker.Approvals[0].TurnKey == worker.CurrentTurn {
		t.Fatalf("worker request lost independent authority: phase=%s steer=%t owner=%s", worker.Phase, worker.CanSteer, worker.Approvals[0].Owner)
	}
	if !telegram.TypingForMainTurn(worker) {
		t.Fatal("production Worker approval snapshot suppressed main typing")
	}
	for _, status := range []string{"sent", "unknown"} {
		view := worker
		view.Approvals = append([]api.Approval(nil), worker.Approvals...)
		view.Approvals[0].Status = status
		if !telegram.TypingForMainTurn(view) {
			t.Fatalf("Worker %s suppressed main typing", status)
		}
	}

	// A child of the resident conversation has a distinct turn key too, but it
	// still blocks the main conversation. The backend's owner decides this.
	s.mu.Lock()
	s.rememberChild("conversation-child")
	s.childRuns["conversation-child"] = "child-turn"
	s.update()
	s.mu.Unlock()
	f.emit(wireMessage{ID: raw("child-approval"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
		"threadId": "conversation-child", "turnId": "child-turn", "itemId": "child-command",
		"command": "synthetic", "availableDecisions": []string{"accept", "decline"},
	})})
	child := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 2 })
	if child.Approvals[1].Owner != "conversation" || child.Approvals[1].TurnKey == child.CurrentTurn || child.Phase != "attention" || child.CanSteer {
		t.Fatalf("conversation child lost blocking scope: phase=%s steer=%t owner=%s", child.Phase, child.CanSteer, child.Approvals[1].Owner)
	}
	child.Phase = "working" // Isolate approval scope from the phase gate.
	if telegram.TypingForMainTurn(child) {
		t.Fatal("main-conversation child approval was ignored by typing")
	}
	child.Approvals[1].Status = "unknown"
	if telegram.TypingForMainTurn(child) {
		t.Fatal("unknown main-conversation child approval was ignored")
	}
	child.Approvals[1].Status = "resolved"
	if !telegram.TypingForMainTurn(child) {
		t.Fatal("resolved child approval suppressed main work")
	}
	child.CurrentTurn = ""
	if telegram.TypingForMainTurn(child) {
		t.Fatal("independent worker alone emitted typing")
	}
}
