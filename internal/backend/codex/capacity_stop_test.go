package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func awaitCapacityOwnerDisconnect(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for c.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("fixture owner did not disconnect")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBotSavedCapacityStopReconnectInterruptsOnlyOriginalTurn(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	request, run := "saved-bot-capacity-request", "saved-bot-turn"
	turn := nativeTurn{ID: run, Status: "inProgress", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}
	interrupts := make(chan struct{ Thread, Turn string }, 2)
	f.mu.Lock()
	f.history = []nativeTurn{turn}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "turn/interrupt" {
			return nil, false
		}
		var p struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		interrupts <- struct{ Thread, Turn string }{p.Thread, p.Turn}
		return map[string]any{}, true
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	s.state.Connection = "offline" // persisted while disconnected; Connect must queue it on ready
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn, s.binding.CapacityStopPhase, s.binding.CapacityStopAccepted = request, "thread-native", run, "requested", true
	receipt := api.Receipt{ID: request, Outcome: "accepted"}
	s.binding.LastReceipt = &receipt
	s.state.LastReceipt = receipt
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case target := <-interrupts:
		if target.Thread != "thread-native" || target.Turn != run {
			t.Fatal("reconnect targeted another Turn", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not reconcile saved stop")
	}
	if err := s.Interrupt(testContext(t)); err == nil {
		t.Fatal("interrupt RPC acknowledgement became terminal success")
	}
	select {
	case target := <-interrupts:
		t.Fatal("repeated stop replayed interrupt", target)
	default:
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: run, Status: "completed"}})})
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && strings.Contains(v.Message, "停止请求未确认及时生效")
	})
	if view.LastReceipt.ID != request || view.LastReceipt.Outcome != "accepted" {
		t.Fatal("original receipt changed during stop", view.LastReceipt)
	}
}

func TestWorkerSavedCapacityStopReconnect(t *testing.T) {
	s, f, _, m := taskPair(t)
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	sendSynthetic(t, s, "saved-worker-parent")
	started := newTask(t, m, "saved-worker-request")
	interrupts := make(chan struct{ Thread, Turn string }, 2)
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method != "turn/interrupt" {
			return original(msg)
		}
		var p struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		interrupts <- struct{ Thread, Turn string }{p.Thread, p.Turn}
		return map[string]any{}, true
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	record := s.binding.Tasks[started.ID]
	thread, run := record.Thread, record.Run
	record.StopPendingRequest, record.StopPendingTurn, record.StopPendingPhase = "saved-worker-request", run, "requested"
	s.state.Connection = "offline"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case target := <-interrupts:
		if target.Thread != thread || target.Turn != run {
			t.Fatal("reconnect targeted another Worker Turn", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not reconcile saved Worker stop")
	}
	if view, err := s.StopWork(testContext(t), started.ID); err == nil || view.Status != "unknown" {
		t.Fatal("Worker stop acknowledged before native terminal", view, err)
	}
	select {
	case target := <-interrupts:
		t.Fatal("repeated Worker stop replayed interrupt", target)
	default:
	}
	// Simulate a missed native notification: only the original owner read can
	// establish terminal status after the interrupt RPC acknowledgement.
	f.mu.Lock()
	worker := f.workers[thread]
	worker.Status.Type = "idle"
	worker.Turns = []nativeTurn{{ID: run, Status: "interrupted", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: "saved-worker-request"}}}}
	f.workers[thread] = worker
	f.mu.Unlock()
	deadline := time.After(2 * time.Second)
	for {
		states := s.WorkStates()
		if len(states) == 1 && states[0].StopResolution == "interrupted" && states[0].Task.Status == "interrupted" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Worker original terminal not recorded", states)
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := s.StopWork(testContext(t), started.ID); err != nil {
		t.Fatal("original Worker interruption not confirmed", err)
	}
}

func TestBotSavedCapacityStopTerminalBeforeInterruptKeepsResult(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	request, run := "terminal-bot-stop-request", "terminal-original-turn"
	turn := nativeTurn{ID: run, Status: "completed", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}
	interrupts := make(chan struct{}, 1)
	f.mu.Lock()
	f.history = []nativeTurn{turn}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			interrupts <- struct{}{}
			return map[string]any{}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	s.state.Connection = "offline"
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn, s.binding.CapacityStopPhase, s.binding.CapacityStopAccepted = request, "thread-native", run, "requested", true
	receipt := api.Receipt{ID: request, Outcome: "accepted"}
	s.binding.LastReceipt = &receipt
	s.state.LastReceipt = receipt
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && strings.Contains(v.Message, "停止请求未确认及时生效")
	})
	if view.LastReceipt.ID != request || view.LastReceipt.Outcome != "accepted" {
		t.Fatal("terminal recovery changed original receipt", view.LastReceipt)
	}
	select {
	case <-interrupts:
		t.Fatal("already completed Turn received an interrupt")
	default:
	}
	if err := s.Interrupt(testContext(t)); err == nil || !strings.Contains(err.Error(), "未确认及时生效") {
		t.Fatal("completed original was falsely called stopped", err)
	}
}

func TestCapacityStopCloseRetainsUnknownTargetWithoutDispatch(t *testing.T) {
	s, f := sessionPair(t, "hold")
	request := "closing-unknown-request"
	interrupts := make(chan struct{}, 1)
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			interrupts <- struct{}{}
			return map[string]any{}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	s.state.Connection = "offline"
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopPhase = request, "thread-native", "requested"
	s.binding.OwnerEndpoint = "unix:///tmp/synthetic-capacity-owner.sock"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupts:
		t.Fatal("close guessed an unidentified Turn")
	default:
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if loaded.loadErr != nil || loaded.binding.CapacityStopRequest != request || loaded.binding.CapacityStopTurn != "" || loaded.binding.OwnerEndpoint != "unix:///tmp/synthetic-capacity-owner.sock" {
		t.Fatal("close discarded unknown stop intent or original owner", loaded.loadErr, loaded.binding.CapacityStopRequest, loaded.binding.CapacityStopTurn, loaded.binding.OwnerEndpoint)
	}
}

func TestCapacityStopNeverUsesAnotherThreadReceipt(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	s.mu.Lock()
	s.binding.CapacityStopRequest = "same-request-on-other-thread"
	s.binding.CapacityStopThread = "old-native-thread"
	s.binding.CapacityStopTurn = "old-native-turn"
	s.binding.CapacityStopPhase = "requested"
	s.binding.ThreadID = "new-native-thread"
	s.state.LastReceipt = api.Receipt{ID: s.binding.CapacityStopRequest, Outcome: "accepted"}
	s.binding.LastReceipt = &s.state.LastReceipt
	s.applyItem("new-native-turn", nativeItem{ID: "new-user", Type: "userMessage", ClientID: s.binding.CapacityStopRequest}, false)
	s.queueCapacityStopsLocked()
	accepted, turn, queued := s.binding.CapacityStopAccepted, s.binding.CapacityStopTurn, s.capacityStopReconciling["bot"]
	s.mu.Unlock()
	if accepted || turn != "old-native-turn" || queued {
		t.Fatal("another thread's same-name receipt authorized the old stop", accepted, turn, queued)
	}
}
