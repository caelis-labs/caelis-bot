package caelis

import (
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestLateLifecycleCannotReprojectRetiredWorkerRun(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	main := s.state.Views["main"]
	main.State.Run = wire.RunState{TurnId: pointer("resident"), Status: pointer("running"), Active: pointer(true)}
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown"}}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("A"), HandleId: pointer("handle-A"), RunId: pointer("run-A"), Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	work := s.state.Views["work"]
	applyEnvelope(work, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("B"), HandleId: pointer("handle-B"), RunId: pointer("run-B"), Lifecycle: &wire.LifecycleEvent{State: "running"}})
	work.State.Run.Status, work.State.Run.Active = pointer("unknown"), pointer(false)
	before := s.WorkStates()[0]
	if before.ExecutionKey != "B" || before.Task.Status != "unknown" || !work.RetiredTurns["A"] {
		t.Fatalf("fixture B: %+v", before)
	}
	applyEnvelope(work, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("A"), HandleId: pointer("handle-A"), RunId: pointer("run-A"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	applyEnvelope(work, wire.Envelope{Kind: "caelis/error", SessionId: pointer("work"), TurnId: pointer("A"), Error: pointer("old failure")})
	after := s.WorkStates()[0]
	if after.ExecutionKey != "B" || after.Task.Status != "unknown" || work.Failure != "" || value(work.State.Run.HandleId) != "handle-B" || value(work.State.Run.RunId) != "run-B" {
		t.Fatalf("late A republished as native head: %+v", after)
	}
	if value(main.State.Run.TurnId) != "resident" || value(main.State.Run.Status) != "running" {
		t.Fatal("Worker lifecycle changed the main turn", main.State.Run)
	}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	work = s.state.Views["work"]
	applyEnvelope(work, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "running"}})
	if got := s.WorkStates()[0]; got.ExecutionKey != "B" || got.Task.Status != "unknown" {
		t.Fatal("restart accepted an old running event", got)
	}
	applyEnvelope(work, wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	if got := s.WorkStates()[0]; got.ExecutionKey != "B" || got.Task.Status != "completed" {
		t.Fatal("real B completion was hidden", got)
	}
}

func TestLateLifecycleCannotCompleteNewResidentTurn(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	v := s.state.Views["main"]
	v.State.Run = wire.RunState{TurnId: pointer("A"), Status: pointer("unknown"), Active: pointer(false)}
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "running"}})
	v.State.Run.Status, v.State.Run.Active = pointer("unknown"), pointer(false)
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	if value(v.State.Run.TurnId) != "B" || value(v.State.Run.Status) != "unknown" {
		t.Fatal("old resident completion replaced B", v.State.Run)
	}
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "failed", Reason: pointer("current failure")}})
	if value(v.State.Run.TurnId) != "B" || value(v.State.Run.Status) != "failed" || v.Failure != "current failure" {
		t.Fatal("current failure was suppressed", v.State.Run, v.Failure)
	}
}

func TestLateTerminalCannotReplaceRestoredWorkerHead(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: "work"}, Task: api.Task{ID: "task", Status: "unknown"}}
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Run: wire.RunState{TurnId: pointer("B"), Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
	applyEnvelope(s.state.Views["work"], wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer("work"), TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	if got := s.WorkStates()[0]; got.ExecutionKey != "B" || got.Task.Status != "unknown" {
		t.Fatal("late A replaced a restored B head", got)
	}
}
