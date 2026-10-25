package codex

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func decideWorkerFixture(t *testing.T, s *Session, f *sessionFixture, task string, n int) api.WorkDecision {
	t.Helper()
	thread, run := workerNativeTarget(t, s, task)
	nativeID := fmt.Sprintf("receipt-native-request-%03d", n)
	f.emit(wireMessage{ID: raw(nativeID), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "receipt fixture", "url": "https://example.invalid/receipt-fixture", "message": "Synthetic request"})})
	var approval string
	awaitState(t, s, func(v api.Snapshot) bool {
		for _, a := range v.Approvals {
			if a.NativeID == string(raw(nativeID)) && a.Status == "pending" {
				approval = a.ID
				return true
			}
		}
		return false
	})
	in := api.WorkDecision{TaskID: task, RequestID: fmt.Sprintf("receipt-decision-%03d", n), Decision: api.Decision{ID: approval, Choice: "accept"}}
	if err := s.DecideWork(testContext(t), in); err != nil {
		t.Fatal("native decision", n, err)
	}
	answer := <-f.answers
	if string(answer.ID) != string(raw(nativeID)) {
		t.Fatal("native request identity changed")
	}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": thread, "requestId": nativeID})})
	return in
}

func TestWorkerReceiptHistoryDoesNotConsumePromptOrStopAdmission(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "receipt-limits-parent")
	task := newTask(t, m, "receipt-limits-worker")
	var first api.WorkDecision
	// This is native request/response traffic, not synthetic map entries. The
	// previous implementation consumed both unrelated lifetime quotas here.
	for i := range 255 {
		in := decideWorkerFixture(t, s, f, task.ID, i)
		if i == 0 {
			first = in
		}
		if i == 99 {
			if _, err := s.SendWork(testContext(t), api.TaskMessage{ID: task.ID, RequestID: "receipt-limits-continue", Prompt: "Continue the same fixture task"}); err != nil {
				t.Error("native approval history blocked prompt continuation:", err)
			}
		}
	}
	var interrupts atomic.Int32
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "turn/interrupt" {
			interrupts.Add(1)
			return map[string]any{}, true
		}
		return original(msg)
	}
	f.mu.Unlock()
	obs, err := s.ObserveWork(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	stop := api.WorkStop{ID: task.ID, RequestID: "receipt-limits-stop", ExecutionKey: obs.State.StopExecutionKey}
	if _, err := s.StopWorkRequest(testContext(t), stop); err != nil || interrupts.Load() != 1 {
		t.Fatal("native control history blocked stop:", err, "interrupts:", interrupts.Load())
	}
	q := api.WorkOperationQuery{TaskID: task.ID, RequestID: first.RequestID, Kind: "decision", Fingerprint: api.WorkFingerprint(first)}
	if got, err := s.ReadWorkOperation(testContext(t), q); err != nil || got.Outcome != "accepted" {
		t.Fatal("old control receipt lost:", got, err)
	}
	if err := s.DecideWork(testContext(t), first); err != nil {
		t.Fatal("settled decision receipt lost:", err)
	}
	select {
	case <-f.answers:
		t.Fatal("settled control replayed")
	default:
	}
}

func TestWorkerKnownDecisionRefusalIsRejectedAndRetainsIntent(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "receipt-refusal-parent")
	task := newTask(t, m, "receipt-refusal-worker")
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{ID: raw("receipt-refusal-native"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "receipt fixture", "url": "https://example.invalid/receipt-fixture", "message": "Synthetic request"})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	in := api.WorkDecision{TaskID: task.ID, RequestID: "receipt-refusal-original", Decision: api.Decision{ID: v.Approvals[0].ID, Choice: "not-offered"}}
	err := s.DecideWork(testContext(t), in)
	var invalid api.DecisionValidationError
	if !errors.As(err, &invalid) {
		t.Error("known refusal did not preserve validation semantics:", err)
	}
	q := api.WorkOperationQuery{TaskID: task.ID, RequestID: in.RequestID, Kind: "decision", Fingerprint: api.WorkFingerprint(in)}
	if got, err := s.ReadWorkOperation(testContext(t), q); err != nil || got.Outcome != "rejected" {
		t.Error("known pre-dispatch refusal became unknown:", got, err)
	}
	if err := s.DecideWork(testContext(t), in); !errors.As(err, &invalid) {
		t.Error("original refusal lost semantics:", err)
	}
	changed := in
	changed.Decision.Choice = "accept"
	if err := s.DecideWork(testContext(t), changed); err == nil {
		t.Fatal("same operation ID accepted a new intent")
	}
	missing := api.WorkDecision{TaskID: task.ID, RequestID: "receipt-missing-original", Decision: api.Decision{ID: "no-native-pending-request", Choice: "accept"}}
	if err := s.DecideWork(testContext(t), missing); !errors.As(err, &invalid) {
		t.Error("missing native request did not refuse before dispatch:", err)
	}
	q.RequestID, q.Fingerprint = missing.RequestID, api.WorkFingerprint(missing)
	if got, err := s.ReadWorkOperation(testContext(t), q); err != nil || got.Outcome != "rejected" {
		t.Error("missing native request refusal became unknown:", got, err)
	}
	select {
	case <-f.answers:
		t.Fatal("known refusal reached native owner")
	default:
	}
}
