package caelis

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestUnknownWorkerRetirementRequiresOriginalIdleState(t *testing.T) {
	var active atomic.Bool
	var known atomic.Bool
	var reportedWorking atomic.Bool
	active.Store(true)
	known.Store(true)
	var gets atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
			return
		}
		gets.Add(1)
		state := wire.SessionState{SessionId: "work"}
		if known.Load() {
			state.Run.Active = pointer(active.Load())
		}
		if reportedWorking.Load() {
			state.Run.Status = pointer("working")
		}
		writeFixture(w, state)
	})
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown", Outcome: "unknown"}, PromptID: "original-operation"}
	if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" {
		t.Fatal("active owner was retired", v, err)
	}
	if got := s.WorkStates(); len(got) != 1 || got[0].Activity != "active" {
		t.Fatal("active unknown not counted", got)
	}
	known.Store(false)
	if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" {
		t.Fatal("missing activity proof retired worker", v, err)
	}
	known.Store(true)
	active.Store(false)
	reportedWorking.Store(true)
	if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" {
		t.Fatal("working run status retired under false activity flag", v, err)
	}
	reportedWorking.Store(false)
	if v, err := s.RetireWork(t.Context(), "task"); err != nil || v.Status != "unavailable" {
		t.Fatal("idle owner not retired", v, err)
	}
	if got := s.WorkStates(); len(got) != 1 || got[0].Task.Status != "unavailable" || got[0].Activity != "idle" {
		t.Fatal("retired state not retained", got)
	}
	if w := s.state.Workers["task"]; w.PromptID != "original-operation" || !w.Retired {
		t.Fatal("original operation receipt lost", w)
	}
	if _, err := s.SendWork(t.Context(), api.TaskMessage{ID: "task", RequestID: "new-request", Prompt: "resume"}); err == nil {
		t.Fatal("retired Worker continued")
	}
	if gets.Load() != 4 {
		t.Fatal("native state was not read exactly four times", gets.Load())
	}
}
