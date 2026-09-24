package codex

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestHumanWorkerTurnAndItemResultAreObservedWithoutPolling(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-human")
	v := newTask(t, m, "task-human-shared")
	finishTask(s, f, v.ID, "completed")
	awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "completed" })
	s.mu.Lock()
	thread := s.binding.Tasks[v.ID].Thread
	oldRun := s.binding.Tasks[v.ID].Run
	s.mu.Unlock()
	var reads atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "thread/read" {
			reads.Add(1)
		}
		return previous(msg)
	}
	f.mu.Unlock()
	f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: "human-run", Status: "inProgress"}})})
	awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "working" })
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": "human-run", "item": nativeItem{ID: "human-answer", Type: "agentMessage", Text: "Result from terminal turn"}})})
	// Standard terminal notifications need not repeat their already emitted items.
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: "human-run", Status: "completed"}})})
	awaitState(t, s, func(api.Snapshot) bool {
		return s.WorkStates()[0].Task.Result == "Result from terminal turn" && s.WorkStates()[0].Task.Status == "completed"
	})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: oldRun, Status: "completed"}})})
	// Let a snapshot observer witness a subsequent ordered event before asserting.
	f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: "next-human-run", Status: "inProgress"}})})
	awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "working" })
	ctx, cancel := context.WithTimeout(context.Background(), 650*time.Millisecond)
	defer cancel()
	_, _ = s.WaitSnapshot(ctx, s.Snapshot().Revision) // Longer than the removed 350ms polling interval.
	if reads.Load() != 0 {
		t.Fatal("background worker was polled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding.Tasks[v.ID].Run != "next-human-run" || s.binding.Tasks[v.ID].View.Result != "" {
		t.Fatal("new turn rewound or retained an old result")
	}
}

func TestDispatchReceiptCannotRewindEarlyCompletionOrHumanTurn(t *testing.T) {
	s := NewSession(SessionOptions{})
	s.resetProjection()
	task := &taskRecord{Thread: "owned", Pending: "request-one", View: api.Task{Status: "unknown"}, Requests: map[string]taskReceipt{"request-one": {Outcome: "unknown"}}}
	s.observeTaskTurn(task, nativeTurn{ID: "bot-turn", Status: "inProgress"})
	task.View.Result = "early streamed result"
	s.observeTaskTurn(task, nativeTurn{ID: "bot-turn", Status: "completed"})
	s.childTerminals[opaque("owned", "bot-turn")] = true
	if _, err := s.taskSendResult(task, "request-one", nativeTurn{ID: "bot-turn", Status: "inProgress"}, nil); err != nil {
		t.Fatal(err)
	}
	if task.View.Status != "completed" || task.View.Result != "early streamed result" || task.Pending != "" {
		t.Fatal("early result lost", task)
	}
	s.observeTaskTurn(task, nativeTurn{ID: "human-turn", Status: "inProgress"})
	if _, err := s.taskSendResult(task, "request-one", nativeTurn{ID: "bot-turn", Status: "inProgress"}, nil); err != nil {
		t.Fatal(err)
	}
	if task.Run != "human-turn" || task.View.Status != "working" || task.View.Result != "" {
		t.Fatal("late receipt rewound user turn")
	}
}
