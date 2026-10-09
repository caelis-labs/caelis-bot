package codex

import (
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestLegacyUnknownRecordsRetireOnlyAfterOriginalThreadIdle(t *testing.T) {
	s, f, d, m := taskPair(t)
	aID, bID := "task-01202acb297eea994c1ca4a9e54af36e", "task-d473f8d406bb4911342448ae7e2bb97e"
	aThread, bThread := "01a10f97-b058-7520-919c-51b8d9088ce8", "01a11174-4d01-7eb1-853e-81123df5d27b"
	bRun, pending := "01a1118c-3fef-7022-91d3-4ed9fa4b2df7", "bot96-98-integrate-add100-hold-ci-20261006"
	s.mu.Lock()
	if s.binding.Tasks == nil {
		s.binding.Tasks = map[string]*taskRecord{}
	}
	s.binding.Tasks[aID] = &taskRecord{Thread: aThread, View: api.Task{ID: aID, Status: "unknown", Outcome: "rejected"}, Requests: map[string]taskReceipt{"bot-codex-disconnect-dream-readonly-20261006": {Outcome: "rejected"}}}
	s.binding.Tasks[bID] = &taskRecord{Thread: bThread, Run: bRun, Pending: pending, View: api.Task{ID: bID, Status: "unknown", Outcome: "unknown"}, Requests: map[string]taskReceipt{pending: {Outcome: "unknown", PriorRun: bRun, PriorStatus: "completed"}}}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	a := nativeThread{ID: aThread}
	a.Status.Type = "idle"
	b := nativeThread{ID: bThread, Turns: []nativeTurn{{ID: "active-unknown", Status: "inProgress"}}}
	b.Status.Type = "active"
	f.workers[aThread] = a
	f.workers[bThread] = b
	f.mu.Unlock()
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if v, err := m.RetireTask(testContext(t), bID); err == nil || v.Status == "unavailable" {
		t.Fatal("active unknown task retired", v, err)
	}
	if v, err := m.ReadTask(testContext(t), aID); err != nil || v.Status != "unavailable" || v.Outcome != "rejected" {
		t.Fatal("rejected idle task not retired", v, err)
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 {
		t.Fatal("active unknown did not retain its work slot", page, err)
	}
	f.mu.Lock()
	b.Status.Type = "idle" // A stale idle envelope cannot override an in-progress latest turn.
	f.workers[bThread] = b
	f.mu.Unlock()
	if v, err := m.RetireTask(testContext(t), bID); err == nil || v.Status == "unavailable" {
		t.Fatal("in-progress turn retired under an idle thread envelope", v, err)
	}
	f.mu.Lock()
	b = nativeThread{ID: bThread, Turns: []nativeTurn{{ID: bRun, Status: "completed"}}}
	b.Status.Type = "idle"
	f.workers[bThread] = b
	f.mu.Unlock()
	if v, err := m.ReadTask(testContext(t), bID); err != nil || v.Status != "unavailable" || v.Outcome != "unknown" {
		t.Fatal("unknown continuation borrowed old completion", v, err)
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 {
		t.Fatal("idle records kept capacity", page, err)
	}
	s.mu.Lock()
	retained := s.binding.Tasks[bID]
	if retained.Pending != pending || retained.Requests[pending].Outcome != "unknown" || retained.Run != bRun {
		s.mu.Unlock()
		t.Fatal("original receipt/run lost")
	}
	s.mu.Unlock()
	if _, err := m.SendTask(testContext(t), api.TaskMessage{ID: bID, RequestID: "new-after-retirement", Prompt: "resume"}); err == nil {
		t.Fatal("retired continuation admitted")
	}
	if _, err := m.WorkTerminal(testContext(t), bID); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatal("retired task opened a terminal", err)
	}
	if d.starts != 0 || d.sends != 0 {
		t.Fatal("fixture replayed a native call", d.starts, d.sends)
	}
	restarted := NewSession(s.opts)
	if got := restarted.WorkStates(); len(got) != 2 || got[0].Task.Status != "unavailable" || got[1].Task.Status != "unavailable" {
		t.Fatal("retirement not durable", got)
	}
}
