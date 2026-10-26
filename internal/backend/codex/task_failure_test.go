package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerFailureUsesTypedNativeCodeOnly(t *testing.T) {
	for _, test := range []struct{ info, code string }{
		{`"serverOverloaded"`, "serverOverloaded"},
		{`{"responseTooManyFailedAttempts":{"httpStatusCode":503}}`, "responseTooManyFailedAttempts"},
		{`"futureCode"`, "unknown"}, {`"responseTooManyFailedAttempts"`, "unknown"}, {`{"responseTooManyFailedAttempts":{"httpStatusCode":"bad"}}`, "unknown"}, {`null`, "unknown"}, {`{"serverOverloaded":"malformed"}`, "unknown"}, {`{"responseTooManyFailedAttempts":null}`, "unknown"}, {`123`, "unknown"},
		{`{"serverOverloaded":{},"badRequest":{}}`, "unknown"},
	} {
		f := workerFailure("thread", "turn", &turnError{Message: "Selected model is at capacity. secret-token /private/path", Info: json.RawMessage(test.info)})
		b, _ := json.Marshal(f)
		if f.Code != test.code || strings.Contains(string(b), "secret-token") || f.Retryable != nil || f.WillRetry != nil {
			t.Fatalf("unsafe classification: %s", b)
		}
		if (f.RetryExhausted != nil) != (test.code == "responseTooManyFailedAttempts") {
			t.Fatal("invented retry exhaustion")
		}
	}
}

func TestWorkerFailurePersistsAndFlowsThroughOriginalReadObserveReport(t *testing.T) {
	s, wire, _, manager := taskPair(t)
	sendSynthetic(t, s, "failure-parent")
	task := newTask(t, manager, "failure-native-task")
	s.mu.Lock()
	record := s.binding.Tasks[task.ID]
	thread, turn := record.Thread, record.Run
	s.mu.Unlock()
	failed := nativeTurn{ID: turn, Status: "failed", Error: &turnError{Message: "Selected model is at capacity. secret-token", Info: raw("serverOverloaded")}, Items: []nativeItem{{ID: "progress", Type: "agentMessage", Text: "Earlier progress"}}}
	wire.mu.Lock()
	native := wire.workers[thread]
	native.Status.Type = "idle"
	native.Turns = []nativeTurn{failed}
	wire.workers[thread] = native
	wire.mu.Unlock()
	wire.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": failed})})
	awaitState(t, s, func(api.Snapshot) bool { v := s.WorkStates()[0].Task; return v.Status == "failed" && v.Failure != nil })
	observation, err := manager.ObserveTask(t.Context(), task.ID)
	if err != nil || observation.State.Task.Failure.Code != "serverOverloaded" || observation.State.Task.Outcome != "accepted" {
		t.Fatal(observation, err)
	}
	conversation, err := manager.ReadTaskConversation(t.Context(), task.ID)
	if err != nil || !reflect.DeepEqual(conversation.Failure, observation.State.Task.Failure) {
		t.Fatal(conversation, err)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	retained := loaded.binding.Tasks[task.ID]
	if retained == nil || !reflect.DeepEqual(retained.View.Failure, observation.State.Task.Failure) {
		t.Fatal("adapter persistence lost failure")
	}
	if loaded.observeTaskTurn(retained, nativeTurn{ID: turn, Status: "failed"}, true) {
		t.Fatal("same history changed immutable failure/revision")
	}
	reopened, err := tasks.Open(filepath.Join(filepath.Dir(s.opts.StateFile), "product-tasks.json"), s.workRoot(), "codex", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	finishRoot(wire)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if err := reopened.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	input := string(wire.lastParams["input"])
	starts := wire.starts
	wire.mu.Unlock()
	if !strings.Contains(input, "serverOverloaded") || !strings.Contains(input, task.ID) || strings.Contains(input, "secret-token") {
		t.Fatal("report lost safe original failure", input)
	}
	if err := reopened.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	wire.mu.Lock()
	defer wire.mu.Unlock()
	if wire.starts != starts {
		t.Fatal("read/restart replayed completion report")
	}
}

func TestWorkerFailureRetryAndGenerationFences(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "state.json")})
	defer s.cancelLife()
	task := &taskRecord{Thread: "child", Run: "one", View: api.Task{ID: "task", Status: "working"}, Requests: map[string]taskReceipt{}}
	s.binding.Tasks = map[string]*taskRecord{"task": task}
	s.childEvent(Notification{Method: "error", Params: raw(map[string]any{"error": map[string]any{"message": "secret", "codexErrorInfo": "serverOverloaded"}, "willRetry": true})}, "child", "one")
	if task.View.Failure != nil || task.View.Status != "working" || task.ReportID != "" {
		t.Fatal("intermediate error became terminal")
	}
	s.observeTaskTurn(task, nativeTurn{ID: "one", Status: "failed"})
	if task.View.Failure.Code != "unknown" || task.View.Failure.WillRetry != nil {
		t.Fatal("transient error became an unproven terminal cause")
	}
	task.SupersededRuns = []string{"one"}
	s.observeTaskTurn(task, nativeTurn{ID: "two", Status: "inProgress"})
	s.observeTaskTurn(task, nativeTurn{ID: "one", Status: "failed", Error: &turnError{Info: raw("badRequest")}})
	if task.View.Status != "working" || task.View.Failure != nil {
		t.Fatal("old error poisoned new run")
	}
	s.childEvent(Notification{Method: "error", Params: raw(map[string]any{"error": map[string]any{"codexErrorInfo": "serverOverloaded"}, "willRetry": false})}, "child", "two")
	s.observeTaskTurn(task, nativeTurn{ID: "two", Status: "failed", Error: &turnError{Info: raw("serverOverloaded")}})
	if f := task.View.Failure; f.WillRetry == nil || *f.WillRetry || f.Retryable != nil {
		t.Fatal("willRetry false became retryable false")
	}
}

func TestWorkerTerminalBeforeAcceptanceKeepsFailure(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "state.json")})
	defer s.cancelLife()
	task := &taskRecord{Thread: "child", Pending: "input", View: api.Task{ID: "task", Status: "unknown"}, Requests: map[string]taskReceipt{"input": {Outcome: "unknown"}}}
	s.binding.Tasks = map[string]*taskRecord{"task": task}
	s.observeTaskTurn(task, nativeTurn{ID: "one", Status: "failed", Error: &turnError{Info: raw("serverOverloaded")}})
	if task.View.Status != "unknown" {
		t.Fatal("terminal settled uncertain input")
	}
	task.Pending = ""
	task.Requests["input"] = taskReceipt{Outcome: "accepted"}
	s.observeTaskTurn(task, nativeTurn{ID: "one", Status: "inProgress"})
	ended := s.childTerminalStatus[opaque("child", "one")]
	s.observeTaskTurn(task, nativeTurn{ID: "one", Status: ended.Status})
	if task.View.Failure == nil || task.View.Failure.Code != "serverOverloaded" {
		t.Fatal("status-only replay lost failure")
	}
}

func TestAcknowledgedWorkerStopSuppressesOnlyItsInterruption(t *testing.T) {
	for _, test := range []struct {
		name, outcome, status string
		wantReport            bool
	}{
		{"confirmed", "accepted", "interrupted", false},
		{"completion_race", "accepted", "completed", true},
		{"failure_race", "accepted", "failed", true},
		{"unknown", "unknown", "interrupted", true},
		{"rejected", "rejected", "interrupted", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, f, _, m := taskPair(t)
			sendSynthetic(t, s, "stop-report-parent")
			task := newTask(t, m, "stop-report-worker")
			f.mu.Lock()
			previous := f.handle
			f.handle = func(w wireMessage) (any, bool) {
				if w.Method == "turn/interrupt" {
					switch test.outcome {
					case "unknown":
						return []string{"invalid acknowledgement"}, true
					case "rejected":
						return &NativeError{Code: -32602, Message: "rejected"}, true
					default:
						return struct{}{}, true
					}
				}
				return previous(w)
			}
			f.mu.Unlock()
			_, err := m.StopTask(testContext(t), task.ID)
			if (err == nil) != (test.outcome == "accepted") {
				t.Fatal("stop response changed", err)
			}
			finishTask(s, f, task.ID, test.status)
			awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == test.status })
			loaded := NewSession(s.opts)
			defer loaded.cancelLife()
			if loaded.workerStopAcknowledged(loaded.binding.Tasks[task.ID]) != (test.outcome == "accepted") {
				t.Fatal("restart lost stop receipt")
			}
			finishRoot(f)
			awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
			f.mu.Lock()
			before := f.starts
			f.mu.Unlock()
			if err := m.DeliverTaskReport(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			reported := f.starts > before
			f.mu.Unlock()
			if reported != test.wantReport {
				t.Fatal("wrong stop notification", reported)
			}
		})
	}
}

func TestCapacityStopTerminalBeforeAckRetainsConfirmation(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "capacity-ack-parent")
	task := newTask(t, m, "capacity-ack-worker")
	s.mu.Lock()
	record := s.binding.Tasks[task.ID]
	thread, turn := record.Thread, record.Run
	record.StopPendingRequest, record.StopPendingTurn, record.StopPendingPhase = "original-input", turn, "requested"
	record.Requests["original-input"] = taskReceipt{Outcome: "accepted"}
	record.Requests["original-stop"] = taskReceipt{Kind: "stop", Outcome: "unknown", ControlTarget: "original-input", Phase: "capacity", ExecutionKey: "pre-admission-key"}
	s.mu.Unlock()
	f.mu.Lock()
	previous := f.handle
	f.handle = func(w wireMessage) (any, bool) {
		if w.Method == "turn/interrupt" {
			s.mu.Lock()
			s.observeTaskTurn(record, nativeTurn{ID: turn, Status: "interrupted"})
			_ = s.save()
			s.mu.Unlock()
			return struct{}{}, true
		}
		return previous(w)
	}
	f.mu.Unlock()
	s.reconcileCapacityStopOnce("task:"+task.ID, thread, "original-input", turn)
	s.mu.Lock()
	ack := s.workerStopAcknowledged(record)
	phase := record.StopPendingPhase
	s.mu.Unlock()
	if !ack || phase != "acknowledged" {
		t.Fatal("terminal-before-ACK discarded confirmation", phase)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if !loaded.workerStopAcknowledged(loaded.binding.Tasks[task.ID]) {
		t.Fatal("confirmed target not durable")
	}
}

func TestWorkerNativeCreationRejectionRemainsDistinctFromUnknown(t *testing.T) {
	for _, rejected := range []bool{true, false} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			s, f, _, m := taskPair(t)
			sendSynthetic(t, s, "creation-rejection-parent")
			f.mu.Lock()
			previous := f.handle
			f.handle = func(w wireMessage) (any, bool) {
				if w.Method == "thread/start" {
					if rejected {
						return &NativeError{Code: -32602, Message: "invalid workspace"}, true
					}
					return map[string]any{}, true
				}
				return previous(w)
			}
			f.mu.Unlock()
			view, err := m.StartTask(testContext(t), api.TaskStart{RequestID: "original-create", Title: "Create", Prompt: "Create"})
			var refusal *api.WorkMessageRejection
			if errors.As(err, &refusal) != rejected || (view.Outcome == "rejected") != rejected {
				t.Fatal(view, err)
			}
			same, err := m.StartTask(testContext(t), api.TaskStart{RequestID: "original-create", Title: "Create", Prompt: "Create"})
			if err != nil || same.ID != view.ID || same.Outcome != view.Outcome {
				t.Fatal("original receipt changed", same, err)
			}
		})
	}
}
