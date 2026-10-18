package codex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestQuestionClosureRetainedForOriginalOwnerWithoutStartingIdleWork(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: root, StateFile: filepath.Join(root, "binding.json")}
	s := NewSession(opts)
	s.binding.ThreadID = "resident"
	s.binding.Tasks = map[string]*taskRecord{"worker": {Thread: "original-worker", View: api.Task{ID: "worker", Status: "completed"}, AsyncQuestions: map[string]workerAsyncQuestion{"q": {CallID: "native-call", Questions: []api.AsyncQuestion{{Title: "Choose"}}}}}}
	notice := api.QuestionClosure{ID: "original-closure", Owner: "codex", WorkID: "worker", CallID: "native-call", State: "dismissed", Reason: "user_dismissed"}
	if err := s.ReceiveQuestionClosure(t.Context(), notice); err != nil {
		t.Fatal(err)
	}
	reopened := NewSession(opts)
	if reopened.loadErr != nil {
		t.Fatal(reopened.loadErr)
	}
	context := reopened.questionClosureContextLocked("original-worker")
	if !strings.Contains(context, "native-call") || !strings.Contains(context, "blocked") || strings.Contains(context, "question_reply") || strings.Contains(context, `"answer"`) {
		t.Fatal(context)
	}
	if reopened.questionClosureContextLocked("resident") != "" || reopened.binding.QuestionClosures[notice.ID].Attempted {
		t.Fatal("retargeted or woke idle owner")
	}
	if err := reopened.ReceiveQuestionClosure(t.Context(), notice); err != nil || len(reopened.binding.QuestionClosures) != 1 {
		t.Fatal(err)
	}
	wrong := notice
	wrong.ID = "foreign"
	wrong.WorkID = "not-owned"
	if err := reopened.ReceiveQuestionClosure(t.Context(), wrong); err == nil {
		t.Fatal("foreign owner accepted")
	}
}
func TestActiveWorkerClosureSteersOnceAndNeverBecomesAnAnswer(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "question-closure-parent")
	v := newTask(t, m, "question-closure-worker")
	s.mu.Lock()
	task := s.binding.Tasks[v.ID]
	thread, run := task.Thread, task.Run
	task.AsyncQuestions = map[string]workerAsyncQuestion{"q": {CallID: "original-call", Run: run, Questions: []api.AsyncQuestion{{Title: "Choose"}}}}
	s.mu.Unlock()
	f.mu.Lock()
	before := d.sends
	d.uncertain = true
	f.mu.Unlock()
	notice := api.QuestionClosure{ID: "original-notice", WorkID: v.ID, Owner: "codex", CallID: "original-call", State: "expired", Reason: "deadline"}
	if err := s.ReceiveQuestionClosure(t.Context(), notice); err != nil {
		t.Fatal(err)
	}
	if err := s.ReceiveQuestionClosure(t.Context(), notice); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d.sends != before+1 || d.turnParams["threadId"] != thread || d.turnParams["expectedTurnId"] != run {
		t.Fatal("closure replayed or started a turn", d.sends, d.turnParams)
	}
	input := d.turnParams["input"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(input, "host_question_lifecycle") || strings.Contains(input, "send_user_message_question_reply") {
		t.Fatal(input)
	}
}
