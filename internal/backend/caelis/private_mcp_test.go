package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestPrivateMCPConsumesOnlyCurrentApprovedAction(t *testing.T) {
	path := "/api/control/v1/sessions/main"
	server := wire.SessionMCPServer{Name: privateTasksService, Transport: "stdio", Command: pointer("fixture"), Args: []string{"scope"}, WorkDir: pointer(t.TempDir())}
	var calls atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case path + "/configuration":
			writeFixture(w, wire.SessionConfiguration{SessionId: "main", Profile: &wire.SessionProfile{McpServers: []wire.SessionMCPServer{server}}})
		case path + "/state":
			writeFixture(w, wire.SessionState{SessionId: "main", Run: wire.RunState{Active: pointer(true), TurnId: pointer("turn")}})
		default:
			t.Error("unexpected private admission request", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	scope := &privateMCP{session: "main", principal: "owner", instance: "instance", client: s.client, server: server}
	s.privateMCP = scope
	fact := reviewFact{
		Review: api.Review{ID: "review", Status: "approved", Action: `{"tool":"caelis_tasks__bot_delegate","arguments":{"prompt":"fixture"}}`},
		TurnID: "turn", ApprovalID: "approval", ItemID: "item", ToolCallID: "core-call",
	}
	s.state.Views["main"].Reviews = map[string]reviewFact{fact.ID: fact}
	args := json.RawMessage(`{"prompt":"fixture"}`)
	for _, foreign := range []privateMCP{
		{session: "worker", principal: "owner", instance: "instance", client: s.client},
		{session: "main", principal: "other", instance: "instance", client: s.client},
		{session: "main", principal: "owner", instance: "another-host", client: s.client},
	} {
		s.privateMCP = &foreign
		if _, err := s.admitPrivateMCP(t.Context(), &foreign, "bot_delegate", args); err == nil || calls.Load() != 0 {
			t.Fatal("foreign binding acquired private authority")
		}
	}
	s.privateMCP = scope
	for _, invalid := range []struct {
		name string
		args json.RawMessage
	}{{"bot_delegate", json.RawMessage(`{"prompt":"changed"}`)}, {"bot_tasks", args}} {
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		if _, err := s.admitPrivateMCP(ctx, scope, invalid.name, invalid.args); err == nil {
			cancel()
			t.Fatal("approval authorized a different tool or arguments")
		}
		cancel()
	}
	admitted, err := s.admitPrivateMCP(t.Context(), scope, "bot_delegate", args)
	if err != nil {
		t.Fatal(err)
	}
	invocation, ok := api.ToolInvocationFromContext(admitted)
	if !ok || invocation.Session != "main" || invocation.Turn != "turn" || invocation.CallID == fact.ToolCallID || s.WorkAdmission(admitted) != nil {
		t.Fatal("original review did not acquire a Bot submission identity", invocation)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	if _, err := s.admitPrivateMCP(ctx, scope, "bot_delegate", args); err == nil {
		t.Fatal("an already claimed review authorized another effect")
	}
	s.privateMCP = nil
	if s.WorkAdmission(admitted) == nil {
		t.Fatal("revoked scope could still prepare work")
	}
}

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
