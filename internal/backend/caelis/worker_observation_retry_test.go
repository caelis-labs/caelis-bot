package caelis

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestWorkerObservationRetriesOnlySameOwnerProjection(t *testing.T) {
	for _, change := range []string{"once", "continuous", "binding", "receipt", "generation", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			var gets atomic.Int32
			var s *Session
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
					t.Errorf("unexpected effect or owner: %s %s", r.Method, r.URL.Path)
					return
				}
				n := gets.Add(1)
				s.mu.Lock()
				current := s.state.Workers["task"]
				switch change {
				case "once":
					if n == 1 {
						s.state.Views["work"].Observed++
					}
				case "continuous":
					s.state.Views["work"].Observed++
				case "binding":
					current.Binding.PrincipalId = "replacement"
				case "receipt":
					current.PromptID = "replacement"
				case "generation":
					s.generation++
				case "cancelled":
					cancel()
				}
				s.state.Workers["task"] = current
				s.mu.Unlock()
				writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}})
			})
			s.state.Workers["task"] = worker{Binding: wire.SessionBinding{SessionId: "work", PrincipalId: "owner"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted"}, PromptID: "original", Activity: "idle"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("turn")}}, Seen: map[string]bool{}}
			got, err := s.ObserveWork(ctx, "task")
			wantReads := int32(1)
			if change == "once" || change == "continuous" {
				wantReads = 2
			}
			if gets.Load() != wantReads {
				t.Fatalf("unbounded retry or changed owner read: %d", gets.Load())
			}
			if change == "once" {
				if err != nil || got.State.Activity != "idle" || api.TaskFinishToken(got.State) == "" {
					t.Fatal("same-owner read did not recover", got, err)
				}
			} else {
				if err == nil || api.TaskFinishToken(got.State) != "" || s.state.Workers["task"].Activity != "" {
					t.Fatal("failed observation issued finish proof", got, err)
				}
				if errors.Is(err, api.ErrWorkObservationChanged) != (change == "continuous") {
					t.Fatal("projection movement confused with owner failure", err)
				}
			}
		})
	}
}
