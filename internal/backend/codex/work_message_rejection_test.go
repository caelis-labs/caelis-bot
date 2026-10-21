package codex

import (
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWorkerMessageRejectsBeforeDispatchButPreservesRecordedUnknown(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "synthetic-parent-request")
	work := newTask(t, m, "original-start")
	s.mu.Lock()
	if s.binding.LostOwnerTasks == nil {
		s.binding.LostOwnerTasks = map[string]bool{}
	}
	s.binding.LostOwnerTasks[work.ID] = true
	s.mu.Unlock()
	in := api.TaskMessage{ID: work.ID, RequestID: "not-dispatched", Prompt: "Synthetic answer"}
	_, err := s.SendWork(t.Context(), in)
	var rejected *api.WorkMessageRejection
	if !errors.As(err, &rejected) || s.WorkMessageRecorded(in) {
		t.Fatal("pre-dispatch refusal lost its exact-call evidence")
	}
	s.mu.Lock()
	task := s.binding.Tasks[work.ID]
	task.Requests["previous-unknown"] = taskReceipt{Fingerprint: opaque(in.Prompt), Outcome: "unknown", Phase: "dispatching"}
	s.mu.Unlock()
	in.RequestID = "previous-unknown"
	got, err := s.SendWork(t.Context(), in)
	if err != nil || got.Outcome != "unknown" {
		t.Fatal("owner change overwrote an already recorded request")
	}
	f.mu.Lock()
	sends := d.sends
	f.mu.Unlock()
	if sends != 1 {
		t.Fatal("refusal or receipt lookup dispatched a second prompt")
	}
}

func TestWorkerMessageLostNativeReplyRetainsOriginalIDWithoutReplay(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "synthetic-parent-request")
	work := newTask(t, m, "original-start")
	f.mu.Lock()
	d.uncertain = true
	f.mu.Unlock()
	in := api.TaskMessage{ID: work.ID, RequestID: "original-answer-request", Prompt: "Synthetic answer"}
	got, err := m.SendTask(t.Context(), in)
	var rejected *api.WorkMessageRejection
	if err == nil || errors.As(err, &rejected) || got.Outcome != "unknown" {
		t.Fatal("lost native reply classified as a safe rejection")
	}
	if !s.WorkMessageRecorded(in) {
		t.Fatal("original request journal missing")
	}
	_, _ = m.SendTask(t.Context(), in)
	f.mu.Lock()
	sends, id := d.sends, d.turnParams["clientUserMessageId"]
	f.mu.Unlock()
	if sends != 2 || id != in.RequestID {
		t.Fatal("unknown message was replayed or its native ID changed")
	}
}
