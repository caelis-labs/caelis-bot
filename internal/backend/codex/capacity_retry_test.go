package codex

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func capacityRejection() *NativeError {
	return &NativeError{Code: -32000, Message: "Selected model is at capacity. Please try a different model."}
}

func TestCapacityClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *NativeError
		want bool
	}{
		{"native text", capacityRejection(), true},
		{"structured code", &NativeError{Code: -32000, Data: json.RawMessage(`{"codexErrorInfo":"serverOverloaded"}`)}, true},
		{"structured variant", &NativeError{Code: -32000, Data: json.RawMessage(`{"codexErrorInfo":{"serverOverloaded":{}}}`)}, true},
		{"runtime already exhausted", &NativeError{Code: -32000, Message: capacityRejection().Message, Data: json.RawMessage(`{"codexErrorInfo":"responseTooManyFailedAttempts"}`)}, false},
		{"unrelated failure", &NativeError{Code: -32000, Message: "quota exceeded"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := capacityNativeError(tc.err); got != tc.want {
				t.Fatalf("capacity = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestBotCapacityRetryKeepsOriginalRequestAndStopsAtLimit(t *testing.T) {
	for _, succeed := range []bool{true, false} {
		s, f := sessionPair(t, "hold")
		s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
		var mu sync.Mutex
		var ids []string
		f.mu.Lock()
		f.handle = func(m wireMessage) (any, bool) {
			if m.Method != "turn/start" {
				return nil, false
			}
			var p struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			mu.Lock()
			ids = append(ids, p.ClientID)
			count := len(ids)
			mu.Unlock()
			if succeed && count == 3 {
				return map[string]any{"turn": nativeTurn{ID: "capacity-run", Status: "inProgress"}}, true
			}
			return capacityRejection(), true
		}
		f.mu.Unlock()
		got := sendSynthetic(t, s, "same-capacity-request")
		mu.Lock()
		if len(ids) != 3 {
			t.Fatalf("dispatches = %d, want 3", len(ids))
		}
		for _, id := range ids {
			if id != "same-capacity-request" {
				t.Fatal("retry changed original client ID", ids)
			}
		}
		mu.Unlock()
		if succeed && got.Outcome != "accepted" {
			t.Fatal("third attempt not accepted", got)
		}
		if !succeed && (got.Outcome != "rejected" || !strings.Contains(got.Message, "耗尽")) {
			t.Fatal("exhaustion not visible", got)
		}
	}
}

func TestBotCapacityWaitCanBeInterruptedWithoutAnotherDispatch(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.capacityBackoff = func(int) time.Duration { return time.Hour }
	first := make(chan struct{}, 1)
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			first <- struct{}{}
			return capacityRejection(), true
		}
		return nil, false
	}
	f.mu.Unlock()
	done := make(chan api.Receipt, 1)
	go func() {
		r, _ := s.Submit(context.Background(), api.Submission{ID: "cancel-capacity-request", Text: "synthetic"}, nil)
		done <- r
	}()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first dispatch missing")
	}
	deadline := time.After(time.Second)
	for {
		if v := s.Snapshot(); v.CanInterrupt && strings.Contains(v.Message, "正在等待") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("capacity wait was not observable")
		case <-time.After(time.Millisecond):
		}
	}
	if err := s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.Outcome != "rejected" || !strings.Contains(r.Message, "取消") {
			t.Fatal("cancel did not reject the undispatched retry", r)
		}
	case <-time.After(time.Second):
		t.Fatal("capacity wait did not cancel")
	}
	select {
	case <-first:
		t.Fatal("cancel replayed the request")
	default:
	}
}

func TestBotStopAfterRetryDispatchKeepsOriginalUnconfirmed(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
	request := "bot-capacity-dispatched-stop"
	secondEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	interrupts := make(chan struct{ Thread, Turn string }, 2)
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			var p struct {
				Thread string `json:"threadId"`
				Turn   string `json:"turnId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			interrupts <- struct{ Thread, Turn string }{p.Thread, p.Turn}
			return map[string]any{}, true
		}
		if m.Method != "turn/start" {
			return nil, false
		}
		var params struct {
			ClientID string `json:"clientUserMessageId"`
		}
		_ = json.Unmarshal(m.Params, &params)
		if params.ClientID != request {
			t.Fatal("native request ID changed", params.ClientID)
		}
		if calls.Add(1) == 1 {
			return capacityRejection(), true
		}
		secondEntered <- struct{}{}
		<-release
		turn := nativeTurn{ID: "late-root-run", Status: "inProgress", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}
		f.mu.Lock()
		f.history = []nativeTurn{turn}
		f.mu.Unlock()
		return map[string]any{"turn": turn}, true
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	issued := make(chan api.Receipt, 1)
	go func() { r, _ := s.Submit(ctx, api.Submission{ID: request, Text: "synthetic"}, nil); issued <- r }()
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("retry was not dispatched")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- s.Interrupt(ctx) }()
	select {
	case r := <-issued:
		if r.Outcome != "unknown" {
			t.Fatal("dispatched retry was called unsent", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("submission did not settle")
	}
	select {
	case err := <-stopped:
		if err == nil || !strings.Contains(err.Error(), "结果尚未确认") {
			t.Fatal("stop was falsely confirmed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop waited for a late native reply")
	}
	once.Do(func() { close(release) })
	f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "late-root-run", Status: "inProgress", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}})})
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.LastReceipt.ID == request && v.LastReceipt.Outcome == "accepted" && v.Phase == "unknown"
	})
	if !strings.Contains(view.Message, "尚未确认") || calls.Load() != 2 {
		t.Fatal("late acceptance lost original stop uncertainty", view.Message, calls.Load())
	}
	select {
	case target := <-interrupts:
		if target.Thread != "thread-native" || target.Turn != "late-root-run" {
			t.Fatal("stop targeted another native Turn", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first stop did not automatically interrupt the accepted original Turn")
	}
	if err := s.Interrupt(ctx); err == nil {
		t.Fatal("interrupt acknowledgement was treated as a terminal stop")
	}
	select {
	case target := <-interrupts:
		t.Fatal("repeated stop sent another interrupt", target)
	default:
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "late-root-run", Status: "interrupted"}})})
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "interrupted" && strings.Contains(v.Message, "原回合已中断")
	})
	s.mu.Lock()
	stopRequest, stopTurn := s.binding.CapacityStopRequest, s.binding.CapacityStopTurn
	s.mu.Unlock()
	if stopRequest != request || stopTurn != "late-root-run" {
		t.Fatal("stop intent did not retain exact original target", stopRequest, stopTurn)
	}
}

func TestCapacityRetrySkipsOtherErrorsAndStartedTurns(t *testing.T) {
	s, f := sessionPair(t, "hold")
	var calls atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			calls.Add(1)
			return &NativeError{Code: -32000, Message: "quota exceeded"}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	if r := sendSynthetic(t, s, "noncapacity-request"); r.Outcome != "rejected" || calls.Load() != 1 {
		t.Fatal("non-capacity error retried", r, calls.Load())
	}
	s2, f2 := sessionPair(t, "hold")
	if r := sendSynthetic(t, s2, "started-capacity-request"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	f2.emit(wireMessage{Method: "error", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "willRetry": true, "error": turnError{Message: capacityRejection().Message}})})
	f2.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "failed", Error: &turnError{Message: capacityRejection().Message}, Items: []nativeItem{{ID: "tool", Type: "commandExecution", Status: "completed"}}}})})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		v := s2.Snapshot()
		if v.Phase == "failed" {
			if !strings.Contains(v.Message, "核对原工作") {
				t.Fatal("terminal capacity error lacks safe next step", v.Message)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("terminal failure not projected")
		case <-time.After(time.Millisecond):
		}
	}
	f2.mu.Lock()
	starts := f2.starts
	f2.mu.Unlock()
	if starts != 1 {
		t.Fatal("started Turn was replayed", starts)
	}
}

func TestWorkerCapacityRetryUsesOriginalTaskReceipt(t *testing.T) {
	s, f, _, manager := taskPair(t)
	sendSynthetic(t, s, "worker-capacity-parent")
	s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
	f.mu.Lock()
	original := f.handle
	var ids []string
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			var p struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			ids = append(ids, p.ClientID)
			if len(ids) < 3 {
				return capacityRejection(), true
			}
		}
		return original(m)
	}
	f.mu.Unlock()
	v := newTask(t, manager, "worker-capacity-request")
	if len(ids) != 3 || v.Outcome != "accepted" {
		t.Fatal("worker retry failed", ids, v)
	}
	for _, id := range ids {
		if id != "worker-capacity-request" {
			t.Fatal("worker request receipt changed", ids)
		}
	}
}

func TestWorkerCapacitySameRequestAsResidentReceiptStillRetries(t *testing.T) {
	s, f, _, manager := taskPair(t)
	request := "shared-capacity-request"
	if r := sendSynthetic(t, s, request); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
	var calls atomic.Int32
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "turn/start" && calls.Add(1) == 1 {
			return capacityRejection(), true
		}
		return original(msg)
	}
	f.mu.Unlock()
	work := newTask(t, manager, request)
	if work.Outcome != "accepted" || calls.Load() != 2 {
		t.Fatal("resident receipt suppressed the worker retry", work, calls.Load())
	}
}

func TestWorkerCapacityWaitProjectsAndCancelsInAdapter(t *testing.T) {
	s, f, _, manager := taskPair(t)
	sendSynthetic(t, s, "worker-cancel-parent")
	s.capacityBackoff = func(int) time.Duration { return time.Hour }
	var calls atomic.Int32
	f.mu.Lock()
	original := f.handle
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			calls.Add(1)
			return capacityRejection(), true
		}
		return original(m)
	}
	f.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := manager.StartTask(context.Background(), api.TaskStart{RequestID: "worker-capacity-cancel", Title: "Synthetic worker", Prompt: "Synthetic request"})
		done <- err
	}()
	var id string
	deadline := time.After(2 * time.Second)
	for {
		states := s.WorkStates()
		if len(states) == 1 && states[0].Task.Status == "pending" && strings.Contains(states[0].Task.Result, "正在等待") {
			id = states[0].Task.ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("worker capacity wait not observable in adapter")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := s.StopWork(testContext(t), id); err != nil {
		t.Fatal("adapter stop did not cancel capacity wait", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker wait did not exit")
	}
	if calls.Load() != 1 {
		t.Fatal("worker cancellation replayed request", calls.Load())
	}
}
