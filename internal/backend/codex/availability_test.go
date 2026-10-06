package codex

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
	"testing"
	"time"
)

func TestControlSnapshotAndAdmissionRemainBoundedWhileComponentLockIsHeld(t *testing.T) {
	s, _ := sessionPair(t, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	started := time.Now()
	v := s.Snapshot()
	composer := s.ComposerSnapshot()
	recovery := s.RecoveryState()
	if time.Since(started) > 100*time.Millisecond || v.Connection != "ready" || composer.Connection != "ready" || recovery.Fence == "" {
		t.Fatal("control snapshot blocked on component", v.Connection)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if !errors.Is(s.Connect(ctx), context.DeadlineExceeded) {
		t.Fatal("connection admission ignored its deadline")
	}
}

func TestLargeToolFrameKeepsObserverApprovalTerminalAndNextRPCHealthy(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "original-input")
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "tool-original", Type: "mcpToolCall", Result: raw(map[string]string{"image": strings.Repeat("x", 14<<20)})}})})
	f.emit(wireMessage{ID: raw("approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "itemId": "command-original", "availableDecisions": []string{"accept", "decline"}})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	if len(v.Approvals[0].Choices) != 2 {
		t.Fatal("native choices omitted")
	}
	if err := s.Decide(testContext(t), api.Decision{ID: v.Approvals[0].ID, Choice: v.Approvals[0].Choices[1].ID}); err != nil {
		t.Fatal(err)
	}
	reply := <-f.answers
	if string(reply.ID) != `"approval-original"` {
		t.Fatal("lost original decision ID")
	}
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "ready" && v.Phase == "completed" })
	s.mu.Lock()
	c := s.client
	item := s.nativeItems[opaque("run-native", "tool-original")]
	s.mu.Unlock()
	if len(item.Result) != 0 {
		t.Fatal("retained tool bytes")
	}
	var response json.RawMessage
	if err := callDecode(testContext(t), c, "thread/read", map[string]any{"threadId": "thread-native", "includeTurns": false}, &response); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedWorkerReceiptSettlesOnlyProvenUnsentRequest(t *testing.T) {
	for _, phase := range []string{"prepared", "dispatching", ""} {
		task := &taskRecord{Pending: "original", Run: "", View: api.Task{Status: "unknown"}, Requests: map[string]taskReceipt{"original": {Phase: phase, Outcome: "unknown", PriorRun: "completed-run", PriorStatus: "completed", PriorResult: "finished"}}}
		settlePreparedTask(task)
		if phase == "prepared" {
			if task.Pending != "" || task.Run != "completed-run" || task.View.Status != "completed" || task.Requests["original"].Outcome != "rejected" {
				t.Fatal("did not restore proven-unsent receipt", task)
			}
		} else if task.Pending != "original" || task.Requests["original"].Outcome != "unknown" {
			t.Fatal("inferred native dispatch outcome", task)
		}
	}
}
