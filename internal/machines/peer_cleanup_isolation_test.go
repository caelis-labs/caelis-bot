package machines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerpeer"
)

func TestHungPeerCleanupDoesNotDelayLocalOrOtherPeerStop(t *testing.T) {
	target := newDualPeerTarget(t)
	s, manager, _, machine, _ := dualPeerConnect(t, target)
	local := newDualNative("local", "codex")
	local.states["local-task"] = api.WorkState{Task: api.Task{ID: "local-task", Status: "working"}, StopExecutionKey: "local-turn"}
	s.local = local
	other, e := manager.StartTask(t.Context(), api.TaskStart{Machine: machine, RequestID: "other-create", Prompt: "controlled"})
	if e != nil {
		t.Fatal(e)
	}
	native := target.owner(s.state.ProfileID, "codex")
	native.mu.Lock()
	key := native.states[other.ID].StopExecutionKey
	native.mu.Unlock()
	entered, release := make(chan struct{}), make(chan struct{})
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(workerpeer.Response{Contract: api.WorkerContract, Peer: "hung-B"})
	}))
	defer h.Close()
	s.mu.Lock()
	s.state.Profiles["hung"] = profile{View: api.Machine{ID: "hung", ConnectionType: "bot"}, Peer: workerpeer.Connection{Address: h.URL, Peer: "hung-B", Token: "test-only-proof"}}
	s.state.Routes["prepared-task"] = "hung"
	s.state.Runtimes["prepared-task"] = "codex"
	s.state.Reservations["prepared-task"] = true
	s.mu.Unlock()
	cleanup := make(chan error, 1)
	go func() { cleanup <- s.ReleaseWorkPreparation("prepared-task") }()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	stopped := make(chan error, 2)
	var stops sync.WaitGroup
	defer stops.Wait()
	for _, in := range []api.WorkStop{{ID: "local-task", RequestID: "local-stop", ExecutionKey: "local-turn"}, {ID: other.ID, RequestID: "other-stop", ExecutionKey: key}} {
		stops.Go(func() { _, e := s.StopWorkRequest(ctx, in); stopped <- e })
	}
	for range 2 {
		select {
		case e := <-stopped:
			if e != nil {
				t.Error(e)
			}
		case <-ctx.Done():
			t.Error("remote cleanup blocked unrelated native stop")
			goto done
		}
	}
done:
	close(release)
	if e = <-cleanup; e != nil {
		t.Error(e)
	}
}

func TestLatePeerCleanupPreservesDispatchedOrReplacedReservation(t *testing.T) {
	for _, mode := range []string{"dispatch", "replacement", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, e := Open(t.TempDir(), newDualNative("local", "codex"))
			if e != nil {
				t.Fatal(e)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				if mode == "unknown" {
					http.Error(w, "no receipt", 503)
					return
				}
				json.NewEncoder(w).Encode(workerpeer.Response{Contract: api.WorkerContract, Peer: "B"})
			}))
			defer h.Close()
			s.state.Profiles["B"] = profile{View: api.Machine{ID: "B", ConnectionType: "bot"}, Peer: workerpeer.Connection{Address: h.URL, Peer: "B", Token: "fixture"}}
			s.state.Routes["original"] = "B"
			s.state.Runtimes["original"] = "codex"
			s.state.Reservations["original"] = true
			s.state.ReservationKeys = map[string]string{"original": "original-lease"}
			result := make(chan error, 1)
			go func() { result <- s.ReleaseWorkPreparation("original") }()
			<-entered
			if mode == "dispatch" {
				if _, e = s.BindWork(t.Context(), api.TaskStart{Machine: "B"}, "original", "codex"); e != nil {
					t.Fatal(e)
				}
			}
			if mode == "replacement" {
				s.mu.Lock()
				s.state.ReservationKeys["original"] = "new-lease"
				s.mu.Unlock()
			}
			close(release)
			e = <-result
			if mode == "unknown" && e == nil {
				t.Fatal("unknown cleanup inferred success")
			}
			if mode != "unknown" && e != nil {
				t.Fatal(e)
			}
			if s.state.Routes["original"] != "B" || s.state.Runtimes["original"] != "codex" {
				t.Fatal("late cleanup deleted original/changed binding")
			}
			if mode == "dispatch" && s.state.Reservations["original"] {
				t.Fatal("dispatch reverted to preparation")
			}
			if mode == "replacement" && s.state.ReservationKeys["original"] != "new-lease" {
				t.Fatal("replacement lease lost")
			}
			if mode == "unknown" && s.state.ReservationKeys["original"] != "original-lease" {
				t.Fatal("unknown proof lost")
			}
		})
	}
}
