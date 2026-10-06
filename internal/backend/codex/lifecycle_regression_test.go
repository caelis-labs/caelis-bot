package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestReviewDecisionNeverCompletesNativeTurn(t *testing.T) {
	for _, status := range []string{"denied", "timedOut", "aborted", "failed"} {
		for _, outcome := range []string{"completed", "failed", "interrupted"} {
			t.Run(status+"/"+outcome, func(t *testing.T) {
				s := NewSession(SessionOptions{})
				defer s.cancelLife()
				s.binding.ThreadID = "root"
				s.state.Connection = "ready"
				s.applyTurn(nativeTurn{ID: "current", Status: "inProgress"}, false)
				review := func(thread, turn, value string) {
					s.applyEvent(Notification{Method: "item/autoApprovalReview/completed", Params: raw(map[string]any{
						"threadId": thread, "turnId": turn, "reviewId": "review", "review": map[string]string{"status": value},
					})})
					s.update()
				}
				review("root", "current", status)
				v := s.Snapshot()
				if v.Phase != "working" || !v.CanInterrupt || v.CurrentTurn != opaque("current") || len(v.Reviews) != 1 || v.Reviews[0].Status != status {
					t.Fatal("review decision ended work", v.Phase, v.CanInterrupt, v.Reviews)
				}
				s.applyTurn(nativeTurn{ID: "current", Status: outcome}, false)
				s.update()
				v = s.Snapshot()
				if v.Phase != outcome || !v.CanSend || v.CanInterrupt || v.CurrentTurn != "" || v.Reviews[0].Status != status {
					t.Fatal("turn terminal did not converge", v.Phase, v.CanSend, v.CanInterrupt, v.Reviews)
				}
				review("root", "current", "inProgress")
				s.applyTurn(nativeTurn{ID: "current", Status: outcome}, false)
				s.update()
				if v = s.Snapshot(); v.Phase != outcome || v.Reviews[0].Status != status {
					t.Fatal("late review revived work", v)
				}
				s.applyTurn(nativeTurn{ID: "next", Status: "inProgress"}, false)
				s.update()
				review("root", "current", "failed")
				if v = s.Snapshot(); v.CurrentTurn != opaque("next") || v.Reviews[0].TurnKey == v.CurrentTurn {
					t.Fatal("historical review became current", v)
				}
			})
		}
	}
}

func TestWorkerReviewAndApprovalDoNotBlockConversation(t *testing.T) {
	s := NewSession(SessionOptions{})
	defer s.cancelLife()
	s.binding.ThreadID = "root"
	s.state.Connection = "ready"
	s.children["worker"] = true
	s.binding.Tasks = map[string]*taskRecord{"task": {Thread: "worker", View: api.Task{Status: "working"}}}
	s.childRuns["worker"] = "worker-turn"
	s.applyEvent(Notification{Method: "item/autoApprovalReview/started", Params: raw(map[string]any{"threadId": "worker", "turnId": "worker-turn", "reviewId": "review", "review": map[string]string{"status": "inProgress"}})})
	s.prompts["worker-prompt"] = &prompt{thread: "worker", turn: "worker-turn", view: api.Approval{Status: "pending"}}
	s.update()
	v := s.Snapshot()
	if !v.CanSend || v.CanInterrupt || v.Phase == "working" || v.Phase == "attention" || v.CurrentTurn != "" {
		t.Fatal("worker decision occupied idle chat", v)
	}
	s.applyTurn(nativeTurn{ID: "root-turn", Status: "inProgress"}, false)
	s.update()
	v = s.Snapshot()
	if !v.CanSteer || v.Phase != "working" {
		t.Fatal("worker decision blocked the main turn", v.Phase, v.CanSteer)
	}
}

func TestWorkerNativeApprovalDoesNotChangeMainTurnPhase(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "root-with-worker-approval")
	s.mu.Lock()
	s.rememberChild("worker")
	s.binding.Tasks = map[string]*taskRecord{"task": {Thread: "worker", View: api.Task{Status: "working", Title: "worker"}}}
	s.childRuns["worker"] = "worker-turn"
	s.update()
	s.mu.Unlock()
	f.emit(wireMessage{ID: raw("worker-request"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
		"threadId": "worker", "turnId": "worker-turn", "itemId": "command", "command": "echo worker", "availableDecisions": []string{"accept", "decline"},
	})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	if v.Phase != "working" || !v.CanSteer || v.Approvals[0].Status != "pending" {
		t.Fatal("worker approval changed main turn", v.Phase, v.CanSteer, v.Approvals)
	}
	if err := s.Decide(testContext(t), api.Decision{ID: v.Approvals[0].ID, Choice: v.Approvals[0].Choices[0].ID}); err != nil {
		t.Fatal(err)
	}
	if answer := <-f.answers; string(answer.ID) != `"worker-request"` {
		t.Fatal("decision lost native request identity", string(answer.ID))
	}
	f.emit(approvalMessage("main-request"))
	v = awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 2 })
	if v.Phase != "attention" || v.CanSteer {
		t.Fatal("main approval did not block its turn", v.Phase, v.CanSteer)
	}
}

func TestStopPrecleanupTimeoutStillDispatchesInterrupt(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "slow-precleanup")
	var interrupts atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/backgroundTerminals/list":
			time.Sleep(2500 * time.Millisecond)
			return map[string]any{"data": []any{}}, true
		case "turn/interrupt":
			var target struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
			}
			_ = json.Unmarshal(m.Params, &target)
			if target.ThreadID == "thread-native" && target.TurnID == "run-native" {
				interrupts.Add(1)
			}
			return map[string]any{}, true // Keep the native turn active after the acknowledgement.
		}
		return nil, false
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_ = s.Interrupt(ctx)
	if interrupts.Load() != 1 {
		t.Fatal("precleanup consumed the stop deadline before turn/interrupt was sent")
	}
	if v := s.Snapshot(); v.CurrentTurn == "" {
		t.Fatal("fixture did not keep the native turn active")
	}
}

func TestCancelledStopAdmissionKeepsOriginalTurnInterruptible(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "retry-unsent-stop")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Interrupt(ctx); err == nil {
		t.Fatal("cancelled stop was reported successful")
	}
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal("cancelled admission damaged the healthy connection", err)
	}
	if v := s.Snapshot(); !v.CanInterrupt || v.CanSend || v.CurrentTurn == "" {
		t.Fatal("definitely unsent stop is not explicitly retryable", v.Phase, v.CanInterrupt)
	}
	if err := s.Interrupt(testContext(t)); err != nil {
		t.Fatal("explicit same-turn retry failed", err)
	}
	if v := s.Snapshot(); !v.CanSend || v.CanInterrupt || v.Phase != "interrupted" {
		t.Fatal("retry did not observe native terminal", v.Phase, v.CanSend, v.CanInterrupt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 1 {
		t.Fatal("stop retry replayed the user turn", f.starts)
	}
}

func TestUnsentStopDoesNotRelabelNaturalCompletion(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "natural-after-unsent-stop")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Interrupt(ctx); err == nil {
		t.Fatal("cancelled stop was reported successful")
	}
	var interrupts atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			interrupts.Add(1)
		}
		return nil, false
	}
	f.mu.Unlock()
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn == "" })
	if err := s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if v := s.Snapshot(); v.Phase != "completed" || !v.CanSend || interrupts.Load() != 0 {
		t.Fatal("never-dispatched stop changed native completion", v.Phase, v.CanSend, interrupts.Load())
	}
}

func TestTerminalCleanupReconcilesExactThreadAndPages(t *testing.T) {
	for _, tc := range []struct {
		name         string
		pages        bool
		terminateErr bool
		cleanErr     bool
		residual     bool
		unsupported  bool
		unloaded     bool
		disconnected bool
		wantErr      bool
	}{
		{name: "empty"},
		{name: "pages", pages: true},
		{name: "natural-exit", terminateErr: true},
		{name: "clean-error-then-empty", cleanErr: true},
		{name: "remaining", residual: true, wantErr: true},
		{name: "unsupported-list", unsupported: true, wantErr: true},
		{name: "unloaded-current", unloaded: true, wantErr: true},
		{name: "disconnect-before-verification", disconnected: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := sessionPair(t, "")
			var mu sync.Mutex
			cleaned, terminated, reads := false, map[string]bool{}, 0
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method != "thread/backgroundTerminals/list" && m.Method != "thread/backgroundTerminals/terminate" && m.Method != "thread/backgroundTerminals/clean" {
					return nil, false
				}
				var p struct{ ThreadID, Cursor, ProcessID string }
				_ = json.Unmarshal(m.Params, &p)
				if p.ThreadID != "thread-native" {
					return &NativeError{Code: -32600, Message: "wrong thread"}, true
				}
				mu.Lock()
				defer mu.Unlock()
				switch m.Method {
				case "thread/backgroundTerminals/list":
					reads++
					if tc.unsupported {
						return &NativeError{Code: -32601, Message: "unsupported"}, true
					}
					if tc.unloaded {
						return &NativeError{Code: -32600, Message: "thread not loaded: thread-native"}, true
					}
					if cleaned && !tc.residual {
						if tc.disconnected {
							f.mu.Lock()
							_ = f.peer.Close()
							f.mu.Unlock()
						}
						return map[string]any{"data": []any{}}, true
					}
					if tc.name == "empty" {
						return map[string]any{"data": []any{}}, true
					}
					if tc.pages && p.Cursor == "" {
						return map[string]any{"data": []map[string]string{{"processId": "one"}}, "nextCursor": "next"}, true
					}
					id := "one"
					if tc.pages {
						id = "two"
					}
					return map[string]any{"data": []map[string]string{{"processId": id}}}, true
				case "thread/backgroundTerminals/terminate":
					terminated[p.ProcessID] = true
					if tc.terminateErr {
						return &NativeError{Code: -32000, Message: "already exited"}, true
					}
					return map[string]bool{"terminated": true}, true
				case "thread/backgroundTerminals/clean":
					cleaned = true
					if tc.cleanErr {
						return &NativeError{Code: -32000, Message: "synthetic"}, true
					}
					return map[string]any{}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			err := s.cleanTerminals(testContext(t), s.client, "thread-native")
			if (err != nil) != tc.wantErr {
				t.Fatal("wrong cleanup result", err)
			}
			mu.Lock()
			if !tc.unsupported && !tc.unloaded && (!cleaned || reads < 2) {
				t.Error("cleanup was not confirmed by a final list", cleaned, reads)
			}
			if tc.pages && (!terminated["one"] || !terminated["two"]) {
				t.Error("not all pages terminated", terminated)
			}
			mu.Unlock()
		})
	}
}

func TestTerminalCleanupSkipsUnloadedHistoricalChild(t *testing.T) {
	s, f := sessionPair(t, "")
	s.mu.Lock()
	s.children["old"] = true
	s.mu.Unlock()
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/backgroundTerminals/list" {
			var p struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if p.ThreadID == "old" {
				return &NativeError{Code: -32600, Message: "thread not loaded: old"}, true
			}
		}
		return nil, false
	}
	f.mu.Unlock()
	if err := s.cleanTerminals(testContext(t), s.client); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalCleanupTimeoutRemainsUnconfirmed(t *testing.T) {
	s, f := sessionPair(t, "")
	blocked := make(chan struct{})
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/backgroundTerminals/list" {
			<-blocked
			return map[string]any{"data": []any{}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.cleanTerminals(ctx, s.client, "thread-native")
	close(blocked)
	if err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("timeout was treated as cleanup proof", err)
	}
}

func TestCleanupUnknownPersistsAndRechecksSameTargetWithoutReplay(t *testing.T) {
	for _, restart := range []bool{false, true} {
		name := "same-connection"
		if restart {
			name = "restart"
		}
		t.Run(name, func(t *testing.T) {
			s, f, _, manager := taskPair(t)
			sendSynthetic(t, s, "root-before-stop")
			task := newTask(t, manager, "worker-survives-cleanup")
			var fail atomic.Bool
			fail.Store(true)
			f.mu.Lock()
			original := f.handle
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/backgroundTerminals/list" && fail.Load() {
					var p struct {
						ThreadID string `json:"threadId"`
					}
					_ = json.Unmarshal(m.Params, &p)
					if p.ThreadID == "thread-native" {
						return &NativeError{Code: -32601, Message: "synthetic unsupported"}, true
					}
				}
				return original(m)
			}
			f.mu.Unlock()
			if err := s.Interrupt(testContext(t)); err == nil {
				t.Fatal("unconfirmed cleanup was reported successful")
			}
			v := s.Snapshot()
			if v.Phase != "unknown" || v.CanSend || v.CanInterrupt || v.Message == "" {
				t.Fatal("uncertain cleanup lost its input fence", v)
			}
			if err := s.Interrupt(testContext(t)); err == nil {
				t.Fatal("repeat stop crossed uncertain receipt")
			}
			b, err := os.ReadFile(s.opts.StateFile)
			if err != nil || !json.Valid(b) || len(s.binding.CleanupTargets) != 1 || s.binding.CleanupTargets[0] != "thread-native" {
				t.Fatal("exact cleanup owner was not durable", err, string(b))
			}
			fail.Store(false)
			owner := s
			if restart {
				owner = NewSession(s.opts)
				owner.start = s.start
				t.Cleanup(func() { _ = owner.Close(testContext(t)) })
			}
			if err := owner.Connect(testContext(t)); err != nil {
				t.Fatal("same-target cleanup did not reconcile", err)
			}
			v = owner.Snapshot()
			if v.Phase != "interrupted" || !v.CanSend || v.CanInterrupt || v.Message != "" || len(owner.binding.CleanupTargets) != 0 {
				t.Fatal("cleanup warning did not converge", v)
			}
			f.mu.Lock()
			starts := f.starts
			f.mu.Unlock()
			if starts != 1 {
				t.Fatal("reconnect replayed the root message", starts)
			}
			states := owner.WorkStates()
			if len(states) != 1 || states[0].Task.ID != task.ID || states[0].StopRequested {
				t.Fatal("worker report ownership changed", states)
			}
		})
	}
}

func TestCleanupReconnectWaitsForNativeTurnTerminal(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "stop-uncertain-root")
	s.mu.Lock()
	s.binding.CleanupTargets = []string{"thread-native"}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	var cleanupCalls atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/backgroundTerminals/list" || m.Method == "thread/backgroundTerminals/clean" {
			cleanupCalls.Add(1)
		}
		return nil, false
	}
	f.mu.Unlock()
	if err := s.Connect(testContext(t)); err == nil {
		t.Fatal("live turn was mistaken for stopped work")
	}
	if cleanupCalls.Load() != 0 || s.Snapshot().CanSend || s.Snapshot().CanInterrupt {
		t.Fatal("recovery cleaned or replayed active work")
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "interrupted"}})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn == "" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if v := s.Snapshot(); v.Phase != "interrupted" || !v.CanSend || cleanupCalls.Load() == 0 {
		t.Fatal("terminal cleanup did not reconcile", v)
	}
}

func TestLiveOwnedConnectionIsNotReplacedAroundIndependentTask(t *testing.T) {
	s, f, _, manager := taskPair(t)
	sendSynthetic(t, s, "root-with-live-task")
	task := newTask(t, manager, "live-task-replacement")
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	var cleanupCalls atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/backgroundTerminals/list" || m.Method == "thread/backgroundTerminals/clean" {
			cleanupCalls.Add(1)
		}
		return nil, false
	}
	connections := f.connections
	f.mu.Unlock()
	s.mu.Lock()
	owner := s.client
	s.state.Connection = "offline" // Synthetic recovery boundary, not a native disconnect.
	s.mu.Unlock()
	if err := s.Connect(testContext(t)); err == nil {
		t.Fatal("live owner was replaced around an independent task")
	}
	s.mu.Lock()
	retained := s.client == owner && len(s.childRuns) == 1 && !s.binding.Tasks[task.ID].SuppressReport
	s.mu.Unlock()
	f.mu.Lock()
	newConnections := f.connections
	f.mu.Unlock()
	if !retained || cleanupCalls.Load() != 0 || newConnections != connections {
		t.Fatal("recovery touched independent task owner", retained, cleanupCalls.Load(), connections, newConnections)
	}
}
