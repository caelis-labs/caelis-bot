package caelis

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type workerAdmissionFixture struct {
	*Session
	starts int
}

func (*workerAdmissionFixture) WorkAdmission(context.Context) error { return nil }
func (f *workerAdmissionFixture) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.starts++
	v := api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	f.mu.Lock()
	f.state.Workers[in.ID] = worker{Task: v}
	f.mu.Unlock()
	return v, nil
}

func TestPendingProjectedUnknownIdleOwnersReleaseFullAdmission(t *testing.T) {
	var gets atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/control/v1/sessions/work-") || !strings.HasSuffix(r.URL.Path, "/state") {
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
			return
		}
		gets.Add(1)
		sid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/control/v1/sessions/"), "/state")
		writeFixture(w, wire.SessionState{SessionId: sid, Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}})
	})
	for i := range 8 {
		id, sid := "task-old-"+string(rune('a'+i)), "work-"+string(rune('a'+i))
		s.state.Workers[id] = worker{Binding: wire.ApplicationBinding{SessionId: sid}, Task: api.Task{ID: id, Status: "pending", Outcome: "accepted"}, PromptID: "original-" + sid}
		s.state.Views[sid] = &view{State: wire.SessionState{SessionId: sid, Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	}
	f := &workerAdmissionFixture{Session: s}
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", f, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 || page.Reserved != 8 {
		t.Fatal("pending native unknowns did not reserve their eight possible slots", page, err)
	}
	newTask, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "new-independent-worker", Title: "New task", Prompt: "produce an isolated artifact"})
	if err != nil || newTask.Status != "working" || f.starts != 1 || gets.Load() != 8 {
		t.Fatal("confirmed idle original owners still blocked admission", newTask, err, f.starts, gets.Load())
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 || page.Reserved != 0 {
		t.Fatal("retired owners kept possible work slots", page, err)
	}
}

// Independent PR #128 review fixture: a successful prompt persists pending,
// while the same owner's current native projection can later be unknown.
func TestPersistedPendingProjectedUnknownRetiresOnlyWithOriginalIdle(t *testing.T) {
	var gets atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		gets.Add(1)
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}})
	})
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending", Outcome: "accepted"}, PromptID: "original-prompt"}
	s.state.Operations["original-prompt"] = journal{Outcome: "unknown", Path: "/sessions/work/prompt"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	if got := s.WorkStates(); len(got) != 1 || got[0].Task.Status != "unknown" || got[0].Activity != "idle" {
		t.Fatalf("wrong precondition: %+v", got)
	}
	got, err := s.RetireWork(t.Context(), "task")
	if err != nil || got.Status != "unavailable" || gets.Load() != 1 {
		t.Fatalf("known idle original still reserved: task=%+v err=%v GET=%d", got, err, gets.Load())
	}
	if got.Outcome != "accepted" || s.state.Workers["task"].PromptID != "original-prompt" || s.state.Operations["original-prompt"].Outcome != "unknown" {
		t.Fatal("original receipt or outcome changed", got, s.state.Workers["task"])
	}
	restarted, err := loadBinding(s.path)
	if err != nil || !restarted.Workers["task"].Retired || restarted.Workers["task"].Task.Status != "unavailable" || restarted.Operations["original-prompt"].Outcome != "unknown" {
		t.Fatal("retirement or receipt not durable", err, restarted.Workers["task"])
	}
	if v, err := s.ReadWork(t.Context(), "task"); err != nil || v.Status != "unavailable" || gets.Load() != 1 {
		t.Fatal("retired read recontacted native owner", v, err, gets.Load())
	}
}

func TestProjectedUnknownNativeEvidenceMatrix(t *testing.T) {
	for _, persisted := range []string{"pending", "unknown", "working"} {
		for _, fact := range []string{"idle", "active", "working-status", "approval", "missing-activity", "unreadable"} {
			t.Run(persisted+"/"+fact, func(t *testing.T) {
				var gets atomic.Int32
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
						t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
						return
					}
					gets.Add(1)
					if fact == "unreadable" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					state := wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown")}}
					if fact != "missing-activity" {
						state.Run.Active = pointer(fact == "active")
					}
					if fact == "working-status" {
						state.Run.Status = pointer("working")
					}
					if fact == "approval" {
						state.Run.WaitingApproval = pointer(true)
					}
					writeFixture(w, state)
				})
				s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: persisted, Outcome: "unknown"}, PromptID: "original-prompt"}
				s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
				got, err := s.RetireWork(t.Context(), "task")
				if gets.Load() != 1 {
					t.Fatal("did not read the exact original owner", gets.Load())
				}
				if fact == "idle" {
					if err != nil || got.Status != "unavailable" || !s.state.Workers["task"].Retired {
						t.Fatal("confirmed idle owner not retired", got, err)
					}
				} else if err == nil || s.state.Workers["task"].Retired || got.Status == "unavailable" {
					t.Fatal("unconfirmed or active original retired", got, err)
				}
				if fact == "active" && s.WorkStates()[0].Activity != "active" {
					t.Fatal("fresh active owner was hidden by stale idle projection", s.WorkStates())
				}
				if s.state.Workers["task"].PromptID != "original-prompt" {
					t.Fatal("original prompt receipt lost")
				}
			})
		}
	}
}

func authorizedWorkerCall(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, invocationKey{}, wire.ApplicationCall{SessionId: s.state.Session.SessionId, ApplicationId: s.state.Connection.ApplicationId, ConnectionId: s.state.Connection.ConnectionId, PrincipalId: s.state.PrincipalID, Source: wire.ApplicationSource{Kind: "user", OperationId: "fixture-user"}})
}

func TestCurrentWorkerProjectionGatesAllContinuationEntries(t *testing.T) {
	for _, status := range []string{"unknown", "working", "completed"} {
		t.Run(status, func(t *testing.T) {
			var gets atomic.Int32
			s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { gets.Add(1) })
			s.state.Workers["task"] = worker{Native: true, Binding: wire.ApplicationBinding{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending"}, PromptID: "original-prompt"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer(status), Active: pointer(status == "working")}}, Seen: map[string]bool{}}
			if status == "unknown" {
				if _, err := s.SendWork(authorizedWorkerCall(t.Context(), s), api.TaskMessage{ID: "task", RequestID: "new-continuation", Prompt: "resume"}); err == nil {
					t.Fatal("projected unknown bypassed continuation fence")
				}
				if _, err := s.WorkTerminal(t.Context(), "task"); err == nil {
					t.Fatal("projected unknown opened native terminal")
				}
			} else if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" || gets.Load() != 0 {
				t.Fatal("nonunknown current projection was retired", v, err, gets.Load())
			}
			if gets.Load() != 0 {
				t.Fatal("continuation or terminal sent a native request", gets.Load())
			}
		})
	}
}

func TestOriginalWorkerReadRejectsChangedOwnerAndProjection(t *testing.T) {
	for _, change := range []string{"binding", "receipt", "active-event", "new-unknown-turn", "generation"} {
		t.Run(change, func(t *testing.T) {
			var s *Session
			s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					return
				}
				s.mu.Lock()
				switch change {
				case "binding":
					current := s.state.Workers["task"]
					current.Binding.SessionId = "replacement"
					s.state.Workers["task"] = current
				case "receipt":
					current := s.state.Workers["task"]
					current.PromptID = "new-prompt"
					s.state.Workers["task"] = current
				case "active-event":
					v := s.state.Views["work"]
					v.Observed++
					v.State.Run.Active = pointer(true)
				case "new-unknown-turn":
					v := s.state.Views["work"]
					v.Observed++
					v.State.Run.TurnId = pointer("new-turn")
				case "generation":
					s.generation++
				}
				s.mu.Unlock()
				writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}})
			})
			s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending"}, PromptID: "original-prompt"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
			if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" || s.state.Workers["task"].Retired {
				t.Fatal("stale original-owner read retired a changed task", v, err)
			}
		})
	}
}

func TestOriginalWorkerReadCannotUseOlderIdleTurn(t *testing.T) {
	var nativeTurn atomic.Value
	nativeTurn.Store("older-A")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer(nativeTurn.Load().(string)), Status: pointer("unknown"), Active: pointer(false)}})
	})
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending"}, PromptID: "later-B-prompt"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("current-B"), Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	if v, err := s.RetireWork(t.Context(), "task"); err == nil || v.Status == "unavailable" || s.state.Workers["task"].Retired {
		t.Fatal("older idle turn retired unresolved current turn", v, err)
	}
	nativeTurn.Store("current-B")
	if v, err := s.RetireWork(t.Context(), "task"); err != nil || v.Status != "unavailable" {
		t.Fatal("current idle turn could not retire unresolved original", v, err)
	}
}

func TestSameOwnerReadAndRetireSerializesContinuation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var gets atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("continuation was dispatched: %s %s", r.Method, r.URL.Path)
			return
		}
		gets.Add(1)
		close(entered)
		select {
		case <-release:
		case <-t.Context().Done():
		}
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}})
	})
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending"}, PromptID: "original-prompt"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	type outcome struct {
		task api.Task
		err  error
	}
	retired := make(chan outcome, 1)
	go func() { v, err := s.RetireWork(t.Context(), "task"); retired <- outcome{v, err} }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal("original owner read did not start")
	}
	sent := make(chan outcome, 1)
	go func() {
		v, err := s.SendWork(authorizedWorkerCall(t.Context(), s), api.TaskMessage{ID: "task", RequestID: "later-continuation", Prompt: "resume"})
		sent <- outcome{v, err}
	}()
	close(release)
	if got := <-retired; got.err != nil || got.task.Status != "unavailable" {
		t.Fatal("idle original was not retired", got)
	}
	if got := <-sent; got.err == nil || got.task.Status != "unavailable" || gets.Load() != 1 {
		t.Fatal("continuation escaped retired original", got, gets.Load())
	}
}

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
