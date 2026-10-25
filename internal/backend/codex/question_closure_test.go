package codex

import (
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyQuestionClosuresAreDroppedWithoutModelInput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "binding.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"threadId":"resident","questionClosures":{"old":{"closure":{"ID":"old","State":"expired"},"thread":"worker","attempted":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{Directory: root, StateFile: path})
	if s.loadErr != nil {
		t.Fatal(s.loadErr)
	}
	if err := s.ReceiveQuestionClosure(t.Context(), api.QuestionClosure{ID: "old", State: "expired"}); err != nil {
		t.Fatal(err)
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "questionClosures") {
		t.Fatal("obsolete model context was retained")
	}
}

func TestWorkerPromptAndAnswerDoNotDependOnResidentTurnOrClosedQuestions(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			s, f, d, m := taskPair(t)
			sendSynthetic(t, s, "parent-original")
			prompt := "  Original assignment\n原文 <&>\t\n"
			v, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "original-work-start", Title: "Original", Prompt: prompt})
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			var inputs []nativeInput
			_ = json.Unmarshal(raw(d.turnParams["input"]), &inputs)
			before := d.sends
			f.mu.Unlock()
			if len(inputs) != 1 || inputs[0].Text != prompt {
				t.Fatal("start prompt changed")
			}
			finishRoot(f)
			awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend && v.Phase == "completed" })
			s.mu.Lock()
			task := s.binding.Tasks[v.ID]
			s.recordWorkerAsync(task, task.Run, workerQuestionFixture())
			for i := 0; i < 200; i++ {
				task.Requests[fmt.Sprint("old-prompt-", i)] = taskReceipt{Kind: "send", Outcome: "accepted", Phase: "settled"}
			}
			// A valid Worker answer can arrive while the resident Bot is idle.
			s.run = ""
			s.binding.DelegationText = ""
			s.mu.Unlock()
			if !active {
				finishTask(s, f, v.ID, "completed")
				awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "completed" })
			}
			if err = s.ReceiveQuestionClosure(t.Context(), api.QuestionClosure{ID: "closed", WorkID: v.ID, State: "dismissed"}); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			after := d.sends
			f.mu.Unlock()
			if after != before {
				t.Fatal("closure sent model input")
			}
			if _, err = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "idle-ordinary-continue", Prompt: "Continue"}); err == nil {
				t.Fatal("ordinary continuation bypassed activation")
			}
			if _, err = m.SendTask(api.WithWorkerQuestionAnswer(t.Context(), "foreign-call"), api.TaskMessage{ID: v.ID, RequestID: "foreign-answer-denied", Prompt: "Answer"}); err == nil {
				t.Fatal("foreign question bypassed binding")
			}
			answer := "<send_user_message_question_reply>\n[{\"answer\":\"Wall\"}]\n</send_user_message_question_reply>"
			in := api.TaskMessage{ID: v.ID, RequestID: "original-answer-request", Prompt: answer}
			result, err := m.SendTask(api.WithWorkerQuestionAnswer(t.Context(), "original-call"), in)
			if err != nil || result.Outcome != "accepted" {
				t.Fatal("valid idle-resident answer rejected", result, err)
			}
			f.mu.Lock()
			_ = json.Unmarshal(raw(d.turnParams["input"]), &inputs)
			after = d.sends
			f.mu.Unlock()
			if len(inputs) != 1 || inputs[0].Text != answer || after != before+1 {
				t.Fatal("answer was wrapped or duplicated")
			}
			_, err = m.SendTask(api.WithWorkerQuestionAnswer(t.Context(), "original-call"), in)
			f.mu.Lock()
			after = d.sends
			f.mu.Unlock()
			if err != nil || after != before+1 {
				t.Fatal("original receipt replayed", err)
			}
			if _, err = m.StartTask(t.Context(), api.TaskStart{RequestID: "idle-new-work-denied", Title: "X", Prompt: "X"}); err == nil {
				t.Fatal("new work bypassed activation")
			}
		})
	}
}

func TestWorkerQuestionPayloadExpiryCannotReviveOrLoseOriginalReceipt(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir(), StateFile: filepath.Join(t.TempDir(), "binding.json")})
	task := &taskRecord{Thread: "original-thread", View: api.Task{ID: "original", Status: "working"}, Requests: map[string]taskReceipt{"answer-unknown": {Kind: "send", Outcome: "unknown", Fingerprint: "original-fingerprint"}}}
	item := workerQuestionFixture()
	if !s.recordWorkerAsync(task, "original-run", item) {
		t.Fatal("question missing")
	}
	for key, q := range task.AsyncQuestions {
		q.ObservedAt = time.Now().Add(-25 * time.Hour).Unix()
		task.AsyncQuestions[key] = q
	}
	s.binding.Tasks = map[string]*taskRecord{"original": task}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if s.recordWorkerAsync(task, "original-run", item) || len(s.WorkStates()[0].AsyncQuestions) != 0 || task.Requests["answer-unknown"].Outcome != "unknown" {
		t.Fatal("expired question revived or original receipt lost")
	}
	reopened := NewSession(s.opts)
	for _, q := range reopened.binding.Tasks["original"].AsyncQuestions {
		if len(q.Questions) != 0 || q.Digest == "" {
			t.Fatal("payload or replay tombstone incorrect")
		}
	}
}

func workerQuestionFixture() nativeItem {
	var item nativeItem
	_ = json.Unmarshal([]byte(`{"id":"original-call","type":"agentMessage","delivery":"async","questions":[{"title":"Original?"}]}`), &item)
	return item
}
