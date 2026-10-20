package machines

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
)

func TestExplicitReadFailureInvalidatesRemoteIdleEvidence(t *testing.T) {
	s := reviewService(t)
	s.state.Routes["original"], s.state.Runtimes["original"] = "machine-remote", "codex"
	s.cacheResponse("machine-remote", "fixture", "codex", []api.WorkState{{Task: api.Task{ID: "original", Status: "unknown", Outcome: "unknown"}, Activity: "idle"}})
	m := reviewManager(t, s)
	s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
		if r.Action != "read" && r.Action != "states" {
			t.Errorf("observation dispatched %s", r.Action)
		}
		return remotework.Response{}, errors.New("fixture offline")
	}
	if _, err := m.ReadTask(t.Context(), "original"); err == nil {
		t.Fatal("read unexpectedly succeeded")
	}
	page, err := m.QueryTasks(api.TaskQuery{})
	if err != nil || page.Reserved != 1 {
		t.Fatal("failed read released unknown capacity using stale idle", page, err)
	}
	for _, state := range s.WorkStates() {
		if state.Task.ID == "original" && (state.Activity != "" || state.Task.Outcome != "unknown") {
			t.Fatal("failed read kept stale evidence or settled original effect")
		}
	}
}

func TestSuccessfulInspectRestartsExhaustedOriginalObservation(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			s := reviewService(t)
			s.state.Routes["original"], s.state.Runtimes["original"] = "machine-remote", "codex"
			s.cacheResponse("machine-remote", "fixture", "codex", []api.WorkState{{Task: api.Task{ID: "original", Status: "working"}, Activity: "active"}})
			key := "machine-remote\x00codex"
			initialRevision := s.cacheRevision[key]
			var online atomic.Bool
			var calls atomic.Int32
			thirdEntered, thirdRelease := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			s.request = func(ctx context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
				switch r.Action {
				case "states":
					call := calls.Add(1)
					if !online.Load() {
						if call == 3 && concurrent {
							close(thirdEntered)
							select {
							case <-thirdRelease:
							case <-ctx.Done():
								return remotework.Response{}, ctx.Err()
							}
						}
						return remotework.Response{}, errors.New("fixture offline")
					}
					return remotework.Response{States: []api.WorkState{{Task: api.Task{ID: "original", Status: "completed"}, Activity: "idle"}}}, nil
				case "inspect":
					out := remotework.Response{}
					out.Setup.State = "ready"
					return out, nil
				default:
					t.Errorf("recovery dispatched %s", r.Action)
					return remotework.Response{}, errors.New("unexpected action")
				}
			}
			done := make(chan struct{})
			go func() { s.Observe(ctx); close(done) }()
			t.Cleanup(func() { cancel(); <-done })
			deadline := time.Now().Add(12 * time.Second)
			for time.Now().Before(deadline) {
				if concurrent {
					select {
					case <-thirdEntered:
						goto exhausted
					default:
					}
				} else {
					s.cacheMu.RLock()
					exhausted := s.cacheRevision[key] == initialRevision+3
					s.cacheMu.RUnlock()
					if exhausted {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		exhausted:
			if calls.Load() != 3 {
				t.Fatal("observation budget not exhausted", calls.Load())
			}
			online.Store(true)
			view, err := s.InspectMachine(t.Context(), "machine-remote", "codex")
			if err != nil || view.State != "ready" {
				t.Fatal(view, err)
			}
			close(thirdRelease) // Old poll must not swallow the explicit recovery revision.
			deadline = time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				for _, state := range s.WorkStates() {
					if state.Task.ID == "original" && state.Task.Status == "completed" {
						if s.route("original") != "machine-remote" {
							t.Fatal("original route replaced")
						}
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("successful explicit recovery did not restore original observation", calls.Load())
		})
	}
}

func TestInspectRenewsOnlyFreshlyConfirmedRuntime(t *testing.T) {
	for _, mode := range []string{"explicit", "single-runtime", "offline", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			s := reviewService(t)
			p := s.state.Profiles["machine-remote"]
			p.View.Available = []string{"codex", "caelis"}
			if mode == "single-runtime" {
				p.View.Available = []string{"caelis"}
			}
			s.state.Profiles["machine-remote"] = p
			calls := 0
			s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
				calls++
				if r.Action != "inspect" || r.Runtime != "caelis" {
					t.Errorf("unexpected recovery %+v", r)
				}
				if mode == "offline" {
					return remotework.Response{}, errors.New("offline")
				}
				out := remotework.Response{}
				out.Setup.State = "ready"
				return out, nil
			}
			runtime := "caelis"
			if mode == "single-runtime" || mode == "ambiguous" {
				runtime = ""
			}
			if _, err := s.InspectMachine(t.Context(), "machine-remote", runtime); err != nil {
				t.Fatal(err)
			}
			want := uint64(0)
			if mode == "explicit" || mode == "single-runtime" {
				want = 1
			}
			if s.cacheRevision["machine-remote\x00caelis"] != want || s.cacheRevision["machine-remote\x00codex"] != 0 {
				t.Fatal("unconfirmed or unrelated Runtime received observation attempts")
			}
			if mode == "ambiguous" && calls != 0 || mode != "ambiguous" && calls != 1 {
				t.Fatal("unexpected inspection count", calls)
			}
		})
	}
}
