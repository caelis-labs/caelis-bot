package caelis

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func TestManagerUnknownContinuationCannotBorrowOldCompletedProjection(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/control/v1/sessions/work/prompt" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		n := posts.Add(1)
		if n == 1 {
			drop(w) // The original request may have reached Core; its result is unknown.
			return
		}
		writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "accepted"})
	})
	s.state.Workers["task"] = worker{
		Native:   true,
		Binding:  wire.ApplicationBinding{SessionId: "work"},
		Task:     api.Task{ID: "task", Status: "pending", Outcome: "accepted"},
		PromptID: "original-start",
	}
	s.state.Views["work"] = &view{State: wire.SessionState{
		SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("old-completed-turn")},
	}, Seen: map[string]bool{}}
	root := t.TempDir()
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 8 })
	first := api.TaskMessage{ID: "task", RequestID: "first-unknown-continuation", Prompt: "first action"}
	if got, err := m.SendTask(authorizedWorkerCall(t.Context(), s), first); err == nil || got.Outcome != "unknown" || posts.Load() != 1 {
		t.Fatalf("original uncertain submission not shaped: task=%+v err=%v POST=%d", got, err, posts.Load())
	}
	if got := s.WorkStates()[0]; got.Task.Status != "unknown" {
		t.Logf("old completed projection still visible: %+v", got)
	}
	if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 || page.Reserved != 1 {
		t.Logf("unknown continuation missing from capacity: page=%+v err=%v", page, err)
	}
	second := api.TaskMessage{ID: "task", RequestID: "second-new-continuation", Prompt: "different action"}
	if got, err := m.SendTask(authorizedWorkerCall(t.Context(), s), second); err == nil || posts.Load() != 1 {
		t.Fatalf("new work dispatched while original continuation is unknown: task=%+v err=%v POST=%d", got, err, posts.Load())
	}
}

func TestOriginalContinuationReceiptSettlesAcrossRestart(t *testing.T) {
	for _, outcome := range []wire.Outcome{"accepted", "rejected"} {
		t.Run(string(outcome), func(t *testing.T) {
			var posts, reads atomic.Int32
			var original atomic.Value
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST" && r.URL.Path == "/api/control/v1/sessions/work/prompt":
					if posts.Add(1) == 1 {
						original.Store(r.Header.Get("Idempotency-Key"))
						drop(w)
						return
					}
					writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "accepted"})
				case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/control/v1/application/operations/"):
					reads.Add(1)
					op := original.Load().(string)
					if r.URL.Path != "/api/control/v1/application/operations/"+op {
						t.Error("read a different receipt", r.URL.Path)
					}
					result := &wire.CommandResult{OperationId: op, Outcome: outcome}
					if outcome == "accepted" {
						result.Target = &wire.CommandTarget{TurnId: pointer("new-turn")}
					}
					writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: outcome, Result: result})
				case r.Method == "GET" && r.URL.Path == "/api/control/v1/sessions/work/state":
					writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("old-turn")}})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			})
			s.state.Workers["task"] = worker{Native: true, Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "accepted", Result: "old result"}, PromptID: "original-start"}
			s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("old-turn")}}, Items: []api.Item{{Kind: "assistant", TurnKey: "old-turn", Text: "old result"}}, Seen: map[string]bool{}}
			root := t.TempDir()
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			m.ConfigureLimit(func() int { return 8 })
			first := api.TaskMessage{ID: "task", RequestID: "first-uncertain-request", Prompt: "first action"}
			if _, err := m.SendTask(authorizedWorkerCall(t.Context(), s), first); err == nil || posts.Load() != 1 {
				t.Fatal("original uncertain POST not recorded", err, posts.Load())
			}
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.client, restored.connected = s.client, true
			m, err = tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", restored, restored, restored.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			m.ConfigureLimit(func() int { return 8 })
			second := api.TaskMessage{ID: "task", RequestID: "second-distinct-request", Prompt: "second action"}
			if got, err := m.SendTask(authorizedWorkerCall(t.Context(), restored), second); err == nil || posts.Load() != 1 {
				t.Fatal("restart bypassed original unknown receipt", got, err, posts.Load())
			}
			if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 0 || page.Reserved != 1 {
				t.Fatal("restart lost possible work slot", page, err)
			}
			got, err := m.ReadTask(t.Context(), "task")
			if err != nil || reads.Load() != 1 || posts.Load() != 1 || restored.state.Workers["task"].PromptID != original.Load().(string) {
				t.Fatal("original receipt was not reconciled by exact ID", got, err, reads.Load(), posts.Load())
			}
			if outcome == "accepted" {
				if got.Status != "pending" {
					t.Fatal("accepted continuation borrowed older terminal turn", got)
				}
				if page, err := m.QueryTasks(api.TaskQuery{}); err != nil || page.Running != 1 || page.Reserved != 0 {
					t.Fatal("accepted pending continuation lost work slot", page, err)
				}
				if _, err := m.SendTask(authorizedWorkerCall(t.Context(), restored), second); err == nil || posts.Load() != 1 {
					t.Fatal("accepted receipt still awaiting its own turn admitted next prompt", err, posts.Load())
				}
				restored.mu.Lock()
				restored.state.Views["work"].State.Run = wire.RunState{Status: pointer("completed"), Active: pointer(false), TurnId: pointer("new-turn")}
				if err := restored.saveLocked(); err != nil {
					restored.mu.Unlock()
					t.Fatal(err)
				}
				restored.mu.Unlock()
				if err := m.RefreshWatchlist(); err != nil {
					t.Fatal(err)
				}
			} else if got.Status != "completed" || got.Outcome != "rejected" || got.Result != "old result" {
				t.Fatal("definite rejection did not restore preceding completion", got)
			}
			if got, err := m.SendTask(authorizedWorkerCall(t.Context(), restored), second); err != nil || posts.Load() != 2 || got.Status == "unknown" {
				t.Fatal("settled original receipt did not allow next request", got, err, posts.Load())
			}
		})
	}
}

func TestUnknownContinuationRequiresReceiptAndCurrentOwnerIdle(t *testing.T) {
	var settled atomic.Bool
	var active atomic.Bool
	active.Store(true)
	var stateReads atomic.Int32
	op := "work-send-" + digest([]byte("unknown-request"))
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/control/v1/application/operations/"+op:
			if !settled.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: "accepted", Result: &wire.CommandResult{OperationId: op, Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("new-turn")}}})
		case r.Method == "GET" && r.URL.Path == "/api/control/v1/sessions/work/state":
			stateReads.Add(1)
			status := "unknown"
			if active.Load() {
				status = "working"
			}
			writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("new-turn"), Status: &status, Active: pointer(active.Load())}})
		default:
			t.Errorf("unexpected owner request %s %s", r.Method, r.URL.Path)
		}
	})
	before := api.Task{ID: "task", Status: "completed", Result: "old answer"}
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown", Outcome: "unknown"}, PromptID: op, Submission: &workerSubmission{OperationID: op, BeforeTurn: "old-turn", BeforeTask: before}}
	s.state.Operations[op] = journal{Path: "/sessions/work/prompt", Outcome: "unknown"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("old-turn"), Status: pointer("completed"), Active: pointer(false)}}, Items: []api.Item{{Kind: "assistant", TurnKey: "old-turn", Text: "old answer"}}, Seen: map[string]bool{}}
	if got, err := s.RetireWork(t.Context(), "task"); err == nil || got.Status != "unknown" || stateReads.Load() != 0 {
		t.Fatal("missing original receipt used old idle turn to retire", got, err, stateReads.Load())
	}
	settled.Store(true)
	if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "working" || s.WorkStates()[0].Activity != "active" {
		t.Fatal("confirmed active original was not counted as work", got, err, s.WorkStates())
	}
	if got, err := s.RetireWork(t.Context(), "task"); err == nil || got.Status == "unavailable" {
		t.Fatal("active original retired", got, err)
	}
	active.Store(false)
	s.mu.Lock()
	s.state.Views["work"].State.Run = wire.RunState{TurnId: pointer("new-turn"), Status: pointer("unknown"), Active: pointer(false)}
	s.mu.Unlock()
	if got, err := s.RetireWork(t.Context(), "task"); err != nil || got.Status != "unavailable" || s.state.Operations[op].Outcome != "accepted" || s.state.Workers["task"].PromptID != op {
		t.Fatal("confirmed idle current generation did not retire with receipt retained", got, err)
	}
}

func TestPreparedContinuationWithoutJournalRollsBackAfterRestart(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("prepared but unsent continuation made owner request %s %s", r.Method, r.URL.Path)
	})
	before := api.Task{ID: "task", Status: "completed", Outcome: "accepted", Result: "old answer"}
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown", Outcome: "unknown"}, PromptID: "work-send-prepared", Submission: &workerSubmission{OperationID: "work-send-prepared", BeforePromptID: "original-start", BeforeTurn: "old-turn", BeforeTask: before}}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("old-turn"), Status: pointer("completed"), Active: pointer(false)}}, Items: []api.Item{{Kind: "assistant", TurnKey: "old-turn", Text: "old answer"}}, Seen: map[string]bool{}}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restarted := New(Options{Directory: filepath.Dir(s.path)})
	restarted.client, restarted.connected = s.client, true
	if got, err := restarted.ReadWork(t.Context(), "task"); err != nil || got.Status != "completed" || got.Result != "old answer" {
		t.Fatal("proven unsent prepared continuation did not restore old generation", got, err)
	}
	if w := restarted.state.Workers["task"]; w.Submission != nil || w.PromptID != "original-start" {
		t.Fatal("unsent intent survived rollback", w)
	}
}

func TestAcceptedContinuationNeedsItsOwnNativeTurnIdentity(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("projection test made native request %s %s", r.Method, r.URL.Path)
	})
	op := "work-send-accepted"
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "pending", Outcome: "accepted"}, PromptID: op, Submission: &workerSubmission{OperationID: op, BeforeTurn: "old-turn", BeforeTask: api.Task{ID: "task", Status: "completed", Result: "old"}}}
	s.state.Operations[op] = journal{Path: "/sessions/work/prompt", Outcome: "accepted"}
	p := &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("old-turn"), Status: pointer("completed"), Active: pointer(false)}}, Seen: map[string]bool{}}
	s.state.Views["work"] = p
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("old terminal turn released accepted new prompt", got)
	}
	p.State.Run.TurnId = pointer("unrelated-turn")
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("unrelated newer turn released accepted prompt", got)
	}
	p.Items = append(p.Items, api.Item{Kind: "user", RequestID: op, TurnKey: "new-turn"})
	p.State.Run.TurnId = pointer("new-turn")
	if got := s.WorkStates()[0].Task.Status; got != "completed" {
		t.Fatal("matching original native input did not settle generation", got)
	}
	p.State.Run.TurnId = pointer("old-turn") // A delayed old snapshot cannot reauthorize the new operation.
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("late old completion overrode current generation", got)
	}
}

func TestAcceptedSteeringNeedsItsOwnNativeInput(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("projection test made native request %s %s", r.Method, r.URL.Path)
	})
	op := "work-send-steering"
	s.state.Workers["task"] = worker{
		Binding:  wire.ApplicationBinding{SessionId: "work"},
		Task:     api.Task{ID: "task", Status: "pending", Outcome: "accepted"},
		PromptID: op,
		Submission: &workerSubmission{OperationID: op, BeforeTurn: "same-turn", Steering: true,
			BeforeTask: api.Task{ID: "task", Status: "completed", Result: "old result"}},
	}
	s.state.Operations[op] = journal{Path: "/sessions/work/prompt", Outcome: "accepted", TurnID: "same-turn"}
	p := &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("same-turn"), Status: pointer("completed"), Active: pointer(false)}}, Seen: map[string]bool{}}
	s.state.Views["work"] = p
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("old terminal of the same turn released accepted steering", got)
	}
	p.State.Run = wire.RunState{TurnId: pointer("same-turn"), Status: pointer("working"), Active: pointer(true)}
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("old active projection admitted another request before steering input", got)
	}
	p.Items = append(p.Items, api.Item{Kind: "user", RequestID: op, TurnKey: "same-turn"})
	if got := s.WorkStates()[0].Task.Status; got != "working" {
		t.Fatal("matching native steering input did not restore active generation", got)
	}
	p.State.Run = wire.RunState{TurnId: pointer("same-turn"), Status: pointer("completed"), Active: pointer(false)}
	if got := s.WorkStates()[0].Task.Status; got != "completed" {
		t.Fatal("matching native steering input did not settle generation", got)
	}
	p.State.Run.TurnId = pointer("unrelated-turn")
	if got := s.WorkStates()[0].Task.Status; got != "pending" {
		t.Fatal("different terminal turn settled accepted steering", got)
	}
}

func TestAcceptedSteeringCanRetireOnlyAfterOriginalOwnerIdle(t *testing.T) {
	var active atomic.Bool
	active.Store(true)
	op := "work-send-steering-idle"
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
			return
		}
		status := "completed"
		if active.Load() {
			status = "working"
		}
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("same-turn"), Status: &status, Active: pointer(active.Load())}})
	})
	s.state.Workers["task"] = worker{
		Binding:  wire.ApplicationBinding{SessionId: "work"},
		Task:     api.Task{ID: "task", Status: "pending", Outcome: "accepted"},
		PromptID: op,
		Submission: &workerSubmission{OperationID: op, BeforeTurn: "same-turn", Steering: true,
			BeforeTask: api.Task{ID: "task", Status: "working"}},
	}
	s.state.Operations[op] = journal{Path: "/sessions/work/steer", Outcome: "accepted", TurnID: "same-turn"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("same-turn"), Status: pointer("working"), Active: pointer(true)}}, Seen: map[string]bool{}}
	if got, err := s.RetireWork(t.Context(), "task"); err == nil || got.Status == "unavailable" {
		t.Fatal("active steering owner retired", got, err)
	}
	active.Store(false)
	if got, err := s.RetireWork(t.Context(), "task"); err != nil || got.Status != "unavailable" || s.state.Workers["task"].PromptID != op || s.state.Operations[op].Outcome != "accepted" {
		t.Fatal("settled steering receipt with explicit idle owner did not safely retire", got, err)
	}
}

func TestLegacyAcceptedReceiptReadsMatchingOriginalTurn(t *testing.T) {
	var current atomic.Bool
	op := "work-send-legacy-accepted"
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/control/v1/sessions/work/state" {
			t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
			return
		}
		turn := "old-turn"
		if current.Load() {
			turn = "new-turn"
		}
		writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: &turn, Status: pointer("completed"), Active: pointer(false)}})
	})
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "completed", Outcome: "unknown"}, PromptID: op}
	s.state.Operations[op] = journal{Path: "/sessions/work/prompt", Outcome: "accepted", TurnID: "new-turn"}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("old-turn"), Status: pointer("completed"), Active: pointer(false)}}, Seen: map[string]bool{}}
	if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "unknown" {
		t.Fatal("old completed turn settled legacy accepted receipt", got, err)
	}
	current.Store(true)
	if got, err := s.ReadWork(t.Context(), "task"); err != nil || got.Status != "completed" {
		t.Fatal("exact accepted target did not restore completed current turn", got, err)
	}
}
