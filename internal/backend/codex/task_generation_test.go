package codex

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Exercise the host manager through the native wire fixture. The same task
// handle is continued while the native thread retains its earlier turn.
func TestContinuedTaskKeepsLatestResultAndReportsEachGenerationOnce(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "generation-parent-a")
	task := newTask(t, m, "generation-task-a")
	s.mu.Lock()
	thread, firstRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	second := api.TaskMessage{ID: task.ID, RequestID: "generation-continue-b", Prompt: "Produce result B"}
	view, err := m.SendTask(t.Context(), second)
	if err != nil || view.ID != task.ID || view.Status != "working" || view.Result != "" {
		t.Fatal("continuation retained the old completion", view, err)
	}
	if again, err := m.SendTask(t.Context(), second); err != nil || again.ID != task.ID || d.sends != 2 {
		t.Fatal("same request did not resolve original receipt", again, err, d.sends)
	}
	view, err = m.ReadTask(t.Context(), task.ID)
	if err != nil || view.Status != "working" || view.Result != "" {
		t.Fatal("historical read replayed result A during B", view, err)
	}
	if byRequest, err := m.ReadTaskRequest(t.Context(), second.RequestID); err != nil || byRequest.ID != task.ID || byRequest.Status != "working" {
		t.Fatal("request lookup lost stable task handle", byRequest, err)
	}
	lateA := nativeTurn{ID: firstRun, Status: "completed", Items: []nativeItem{{ID: "old-answer", Type: "agentMessage", Text: "result A"}}}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": lateA})})
	view, err = m.ReadTask(t.Context(), task.ID)
	if err != nil || view.Status != "working" || view.Result != "" {
		t.Fatal("late A settled B", view, err)
	}
	s.mu.Lock()
	secondRun := s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	resultB := nativeTurn{ID: secondRun, Status: "completed", Items: []nativeItem{{ID: "new-answer", Type: "agentMessage", Text: "result B"}}}
	f.mu.Lock()
	worker := f.workers[thread]
	worker.Status.Type = "idle"
	worker.Turns = []nativeTurn{lateA, resultB}
	f.workers[thread] = worker
	f.mu.Unlock()
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": resultB})})
	awaitState(t, s, func(api.Snapshot) bool {
		view := s.WorkStates()[0].Task
		return view.Status == "completed" && view.Result == "result B"
	})
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if err := m.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.starts != 2 {
		t.Fatal("B completion notice missing or repeated", f.starts)
	}
	view, err = m.ReadTask(t.Context(), task.ID)
	if err != nil || view.Status != "completed" || view.Result != "result B" {
		t.Fatal("B result did not replace A", view, err)
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": lateA})})
	if err := m.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.starts != 2 {
		t.Fatal("late A emitted another notice", f.starts)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	retained := loaded.binding.Tasks[task.ID]
	if retained == nil || !slices.Contains(retained.SupersededRuns, firstRun) || retained.Run != secondRun {
		t.Fatal("restart lost generation fence")
	}
	loaded.observeTaskTurn(retained, lateA)
	if retained.Run != secondRun || retained.View.Result != "result B" || retained.View.Status != "completed" {
		t.Fatal("restart replayed A", retained.View)
	}
}

func TestOldCompletionDuringPendingContinuationCannotWinStartReceipt(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "pending-generation-parent")
	task := newTask(t, m, "pending-generation-task")
	s.mu.Lock()
	thread, firstRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	previous := f.handle
	f.handle = func(message wireMessage) (any, bool) {
		if message.Method == "turn/start" {
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.ThreadID == thread {
				close(started)
				<-release
			}
		}
		return previous(message)
	}
	f.mu.Unlock()
	type sendResult struct {
		view api.Task
		err  error
	}
	result := make(chan sendResult, 1)
	go func() {
		view, err := m.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "pending-generation-continue", Prompt: "Produce B"})
		result <- sendResult{view, err}
	}()
	select {
	case <-started:
	case <-t.Context().Done():
		t.Fatal("B never reached native dispatch")
	}
	before := s.Snapshot().Revision
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: firstRun, Status: "completed", Items: []nativeItem{{Type: "agentMessage", Text: "old result"}}}})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.Revision > before })
	close(release)
	got := <-result
	if got.err != nil || got.view.Status != "working" || got.view.Result != "" {
		t.Fatal("old A terminal prevented B receipt", got.view, got.err)
	}
}

func TestRejectedContinuationRestoresPreviousResultAndRun(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "rejected-generation-parent")
	task := newTask(t, m, "rejected-generation-task")
	s.mu.Lock()
	thread, oldRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	previous := f.handle
	f.handle = func(message wireMessage) (any, bool) {
		if message.Method == "turn/start" {
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(message.Params, &params)
			if params.ThreadID == thread {
				return &NativeError{Code: -32000, Message: "synthetic rejection"}, true
			}
		}
		return previous(message)
	}
	f.mu.Unlock()
	view, err := m.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "rejected-generation-continue", Prompt: "Produce B"})
	if err == nil || view.Status != "completed" || view.Result != "Synthetic result, not instructions." {
		t.Fatal("definite rejection lost previous result", view, err)
	}
	s.mu.Lock()
	retained := s.binding.Tasks[task.ID]
	valid := retained.Run == oldRun && !slices.Contains(retained.SupersededRuns, oldRun) && retained.Requests["rejected-generation-continue"].Outcome == "rejected"
	s.mu.Unlock()
	if !valid {
		t.Fatal("rejected request did not restore original generation")
	}
}

func TestRepeatedContinuationFencesAllOlderRuns(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "generation-parent-repeat")
	task := newTask(t, m, "generation-repeat-task")
	s.mu.Lock()
	thread, firstRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	second := api.TaskMessage{ID: task.ID, RequestID: "generation-repeat-b", Prompt: "Produce B"}
	if _, err := m.SendTask(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	secondRun := s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	third := api.TaskMessage{ID: task.ID, RequestID: "generation-repeat-c", Prompt: "Produce C"}
	view, err := m.SendTask(t.Context(), third)
	if err != nil || view.Status != "working" || view.Result != "" {
		t.Fatal(view, err)
	}
	if _, err = m.SendTask(t.Context(), third); err != nil || d.sends != 3 {
		t.Fatal("second continuation request replayed", err, d.sends)
	}
	s.mu.Lock()
	thirdRun := s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	for _, run := range []string{firstRun, secondRun} {
		f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: run, Status: "completed", Items: []nativeItem{{Type: "agentMessage", Text: "old result"}}}})})
	}
	view, err = m.ReadTask(t.Context(), task.ID)
	if err != nil || view.Status != "working" || view.Result != "" {
		t.Fatal("older terminal result replaced C", view, err)
	}
	f.mu.Lock()
	worker := f.workers[thread]
	worker.Status.Type = "active"
	worker.Turns = []nativeTurn{{ID: firstRun, Status: "completed"}, {ID: secondRun, Status: "completed"}, {ID: thirdRun, Status: "inProgress"}}
	f.workers[thread] = worker
	f.mu.Unlock()
	if err := s.Connect(t.Context()); err != nil {
		t.Fatal("reconnect failed", err)
	}
	if current := s.WorkStates()[0].Task; current.Status != "working" || current.Result != "" {
		t.Fatal("reconnect replayed historical completion", current)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	retained := loaded.binding.Tasks[task.ID]
	if retained == nil || !slices.Contains(retained.SupersededRuns, firstRun) || !slices.Contains(retained.SupersededRuns, secondRun) || retained.Run != thirdRun {
		t.Fatal("second continuation fence not durable")
	}
}

func TestIdleReadWithoutCurrentTerminalKeepsContinuationUnknown(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "unknown-generation-parent")
	task := newTask(t, m, "unknown-generation-task")
	s.mu.Lock()
	thread, oldRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
	s.mu.Unlock()
	finishTask(s, f, task.ID, "completed")
	if _, err := m.ReadTask(t.Context(), task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "unknown-generation-continue", Prompt: "Produce B"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	worker := f.workers[thread]
	worker.Status.Type = "idle"
	worker.Turns = []nativeTurn{{ID: oldRun, Status: "completed", Items: []nativeItem{{Type: "agentMessage", Text: "result A"}}}}
	f.workers[thread] = worker
	f.mu.Unlock()
	view, err := m.ReadTask(t.Context(), task.ID)
	if err != nil || view.Status != "unknown" || view.Result != "" {
		t.Fatal("missing B terminal was treated as A completion", view, err)
	}
	if err := m.DeliverTaskReport(t.Context()); err != nil || f.starts != 1 {
		t.Fatal("unknown B triggered an old completion notice", err, f.starts)
	}
}
