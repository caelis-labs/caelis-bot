package caelis

import (
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestPrivateMCPRecoveryCannotSpendRevokedPreparation(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("revoked preparation made a Core request", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	pending := worker{Native: true, Task: api.Task{ID: "task", Outcome: "unknown"}, Start: &workerStart{PrivateMCP: true}}
	s.state.Workers[pending.Task.ID] = pending
	task, err := s.advanceWorker(t.Context(), pending)
	if err == nil || task.Outcome != "rejected" || s.state.Workers[task.ID].Start != nil || len(s.state.Operations) != 0 {
		t.Fatal("undispatched private preparation retained write authority", task.Outcome, err)
	}
}
