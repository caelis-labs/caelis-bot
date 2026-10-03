package machines

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type reviewLocal struct{ inert }

func (reviewLocal) BindWork(context.Context, api.TaskStart, string, string) (string, error) {
	return "codex", nil
}
func (reviewLocal) OwnsWork(runtime string) bool { return runtime == "codex" }
func (reviewLocal) WorkStates() []api.WorkState {
	return []api.WorkState{{Runtime: "codex", Task: api.Task{ID: "task-local", Status: "working"}}}
}

type reviewReports struct{}

func (reviewReports) SubmitReport(context.Context, api.Submission) (api.Receipt, error) {
	return api.Receipt{}, nil
}

func reviewManager(t *testing.T, s *Service) *tasks.Manager {
	t.Helper()
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", s, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func reviewService(t *testing.T) *Service {
	t.Helper()
	s, err := Open(t.TempDir(), reviewLocal{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["machine-remote"] = profile{View: api.Machine{ID: "machine-remote", Runtime: "codex", State: "ready"}}
	return s
}

func awaitReview(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func TestRemoteObservationDoesNotBlockLocalTaskOperations(t *testing.T) {
	for _, blockedAction := range []string{"states", "inspect"} {
		t.Run(blockedAction, func(t *testing.T) {
			s := reviewService(t)
			s.state.Routes["task-remote"], s.state.Runtimes["task-remote"] = "machine-remote", "codex"
			if blockedAction == "inspect" {
				p := s.state.Profiles["machine-remote"]
				p.View.State = "offline"
				s.state.Profiles["machine-remote"] = p
			}
			m := reviewManager(t, s)
			entered := make(chan struct{})
			s.request = func(ctx context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
				if r.Action == blockedAction {
					close(entered)
					<-ctx.Done()
					return remotework.Response{}, ctx.Err()
				}
				return remotework.Response{}, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			observed := make(chan struct{})
			go func() { s.Observe(ctx); close(observed) }()
			awaitReview(t, entered, "remote observation did not start")
			local := make(chan struct{})
			go func() {
				_, readErr := m.ReadTask(ctx, "task-local")
				_, stopErr := m.StopTask(ctx, "task-local")
				if readErr != nil || stopErr != nil || len(m.ListTasks()) != 1 || len(s.Machines()) != 1 {
					t.Error("local operation failed", readErr, stopErr)
				}
				close(local)
			}()
			awaitReview(t, local, "remote observation blocked local task read/stop/list or machine settings")
			cancel()
			awaitReview(t, observed, "observation cancellation did not finish")
		})
	}
}

func TestLateObservationCannotOverwriteChangedDefault(t *testing.T) {
	s := reviewService(t)
	s.state.Routes["task-remote"], s.state.Runtimes["task-remote"] = "machine-remote", "codex"
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
		if r.Action == "states" {
			close(entered)
			<-release
			return remotework.Response{}, errors.New("old connection failed")
		}
		out := remotework.Response{}
		out.Setup.State = "ready"
		return out, nil
	}
	poll := s.workPolls()[0]
	go func() { s.pollWork(t.Context(), poll); close(done) }()
	awaitReview(t, entered, "poll did not start")
	if _, err := s.InspectMachine(t.Context(), "machine-remote", "caelis"); err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitReview(t, done, "poll did not finish")
	if v := s.Machines()[0]; v.Runtime != "caelis" || v.State != "ready" || v.Issue != "" {
		t.Fatal("stale poll overwrote changed profile", v.Runtime, v.State)
	}
}

func TestLateObservationCannotRewindFreshTaskRead(t *testing.T) {
	s := reviewService(t)
	s.state.Routes["task-remote"], s.state.Runtimes["task-remote"] = "machine-remote", "codex"
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var polls atomic.Int32
	s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
		out := remotework.Response{Task: api.Task{ID: "task-remote", Status: "completed"}}
		if r.Action == "states" {
			if polls.Add(1) == 1 {
				close(entered)
				<-release
				out.Task.Status = "working"
			}
			out.States = []api.WorkState{{Task: out.Task}}
		}
		return out, nil
	}
	poll := s.workPolls()[0]
	go func() { s.pollWork(t.Context(), poll); close(done) }()
	awaitReview(t, entered, "poll did not start")
	if _, err := s.ReadWork(t.Context(), "task-remote"); err != nil {
		t.Fatal(err)
	}
	close(release)
	awaitReview(t, done, "poll did not finish")
	for _, state := range s.WorkStates() {
		if state.Task.ID == "task-remote" && state.Task.Status != "completed" {
			t.Fatal("late observation rewound a fresher native task read")
		}
	}
}

func TestFailedRemotePreparationLeavesNoInvisibleOwner(t *testing.T) {
	for _, failure := range []string{"invalid_workspace", "cancelled", "ledger_write"} {
		t.Run(failure, func(t *testing.T) {
			s := reviewService(t)
			starts := 0
			ledgerRoot := t.TempDir()
			s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
				if r.Action == "start" {
					starts++
				}
				if r.Action == "prepare" {
					if failure == "cancelled" {
						return remotework.Response{}, context.Canceled
					}
					if failure != "ledger_write" {
						return remotework.Response{}, errors.New(failure)
					}
					// Change only the task ledger path into an unwritable directory.
					if err := os.Remove(filepath.Join(ledgerRoot, "tasks.json")); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(ledgerRoot, "tasks.json"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				return remotework.Response{Task: api.Task{Workspace: "/target/work"}}, nil
			}
			// Use an empty local projection so startup does not write tasks.json.
			s.local = inert{}
			m, err := tasks.Open(filepath.Join(ledgerRoot, "tasks.json"), filepath.Join(ledgerRoot, "Tasks"), "codex", s, reviewReports{}, func() api.Snapshot { return api.Snapshot{} })
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.StartTask(t.Context(), api.TaskStart{RequestID: "review-request", Machine: "machine-remote", Title: "Fixture", Prompt: "Fixture"}); err == nil {
				t.Fatal("preparation unexpectedly succeeded")
			}
			if starts != 0 || len(s.state.Routes) != 0 || len(s.state.Runtimes) != 0 || len(s.state.Reservations) != 0 {
				t.Fatal("pre-dispatch failure retained an invisible owner or dispatched work")
			}
			again, err := Open(s.root, inert{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = again.RemoveMachine(t.Context(), "machine-remote"); err != nil {
				t.Fatal("failed preparation made the machine undeletable after restart", err)
			}
		})
	}
}

func TestPreparationCleanupNeverReleasesUnknownDispatchedOwner(t *testing.T) {
	s := reviewService(t)
	s.request = func(_ context.Context, _ profile, r remotework.Request) (remotework.Response, error) {
		return remotework.Response{}, context.DeadlineExceeded
	}
	in := api.WorkStart{TaskStart: api.TaskStart{Machine: "machine-remote"}, ID: "task-unknown"}
	if _, err := s.StartWork(t.Context(), in); err == nil {
		t.Fatal("dispatch unexpectedly succeeded")
	}
	if err := s.ReleaseWorkPreparation(in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareRemoteWork(t.Context(), in.TaskStart, in.ID); err == nil {
		t.Fatal("preparation unexpectedly succeeded")
	}
	again, err := Open(s.root, inert{}, nil)
	if err != nil || again.state.Routes[in.ID] != in.Machine || again.state.Runtimes[in.ID] != "codex" || again.state.Reservations[in.ID] {
		t.Fatal("unknown dispatch lost its durable owner", err)
	}
	if err := again.RemoveMachine(t.Context(), in.Machine); err == nil {
		t.Fatal("removed an unknown execution owner")
	}
}

func TestRestartedPreDispatchReservationIsDeletable(t *testing.T) {
	s := reviewService(t)
	if _, err := s.BindWork(t.Context(), api.TaskStart{Machine: "machine-remote"}, "task-reserved", ""); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.root, inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.RemoveMachine(t.Context(), "machine-remote"); err != nil {
		t.Fatal("crashed preparation cannot be removed", err)
	}
	reopened, err := Open(s.root, inert{}, nil)
	if err != nil || len(reopened.state.Routes) != 0 || len(reopened.Machines()) != 0 {
		t.Fatal("removed reservation returned", err)
	}
}

func TestRestoredVisibleLedgerCannotLoseItsPreDispatchRoute(t *testing.T) {
	s := reviewService(t)
	in := api.TaskStart{Machine: "machine-remote"}
	if _, err := s.BindWork(t.Context(), in, "task-recorded", ""); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.root, inert{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := again.BindWork(t.Context(), in, "task-recorded", "codex"); err != nil {
		t.Fatal(err)
	}
	if err := again.ReleaseWorkPreparation("task-recorded"); err != nil {
		t.Fatal(err)
	}
	if err := again.RemoveMachine(t.Context(), in.Machine); err == nil {
		t.Fatal("visible retained ledger lost its machine")
	}
}

func TestRemovalSaveFailureRetainsPreparedProfileAndRoute(t *testing.T) {
	s := reviewService(t)
	if _, err := s.BindWork(t.Context(), api.TaskStart{Machine: "machine-remote"}, "task-reserved", ""); err != nil {
		t.Fatal(err)
	}
	s.root = filepath.Join(s.root, "unwritable")
	if err := os.WriteFile(s.root, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMachine(t.Context(), "machine-remote"); err == nil {
		t.Fatal("removal unexpectedly saved")
	}
	if len(s.Machines()) != 1 || s.state.Routes["task-reserved"] != "machine-remote" || !s.state.Reservations["task-reserved"] {
		t.Fatal("failed removal discarded profile or pre-dispatch route")
	}
}
