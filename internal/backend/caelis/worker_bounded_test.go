package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Synthetic protocol regression. Actual pinned Core and strict SSH evidence is
// exercised separately; this protects bounded scope/protocol and replay gates.
func TestBoundedWorkerProtocolCreationScopeAndPinnedReconnect(t *testing.T) {
	ctx := t.Context()
	directory := filepath.Join(t.TempDir(), "private")
	source := api.WorkDispatchSource{NodeID: "local", Backend: "codex", BindingID: "native-primary", OperationID: "native-activation", Kind: "native_activation"}
	target := api.WorkTarget{NodeID: "remote", Backend: "caelis", Role: api.RoleWorker}
	var creates, enrolls atomic.Int32
	var profile wire.ApplicationProfile
	var corrupted atomic.Bool
	var secret string
	binding := func() wire.ApplicationBinding {
		creation := digest([]byte("native immutable creation"))
		if corrupted.Load() {
			creation = "changed"
		}
		return wire.ApplicationBinding{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", SessionId: "bounded-task", Profile: profile, CreationDigest: creation}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("bounded call escaped independent application scope")
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case path == "/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: append(append([]string{}, boundedWorkerRequired...), workerRequired...)})
		case path == "/application/connection":
			writeFixture(w, wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)})
		case path == "/application/sessions" && r.Method == "POST":
			creates.Add(1)
			var req wire.CreateApplicationSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			profile = req.Profile
			if profile.Execution != "workspace-write" || profile.Workspace == nil || value(profile.Workspace.Cwd) != "/remote/target-workspace" || profile.Permissions == nil || value(profile.Permissions.Mode) != "workspace-write" || value(profile.Permissions.ApprovalMode) != "manual" || profile.Reviewer != nil || len(profile.Tools) != 0 || profile.Inherit.CwdInstructions || profile.Inherit.Mcp || profile.Inherit.Skills || profile.Inherit.WorkspaceMemory {
				t.Error("bounded native profile widened scope or inherited Bot authority")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("bounded-task")})
		case path == "/application/sessions/bounded-task":
			writeFixture(w, binding())
		case path == "/application/sessions/bounded-task/background-grants":
			var req wire.ApplicationBackgroundGrantRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.AuthorizationOperationId != source.OperationID || req.Source == "" {
				t.Error("bounded activation invented a native user prompt")
			}
			writeFixture(w, wire.ApplicationBackgroundGrant{Id: "original-grant", PrincipalId: "owner", ApplicationId: "app", ConnectionId: "connection", SessionId: "bounded-task", Source: req.Source, AuthorizationOperationId: req.AuthorizationOperationId})
		case path == "/application/sessions/bounded-task/prompt":
			var req wire.ApplicationPromptRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.SourceKind != "authorized_background" || value(req.GrantId) != "original-grant" {
				t.Error("bounded task promoted source to a native user")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed", SessionId: pointer("bounded-task")})
		default:
			t.Error("unexpected bounded native call", path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	endpoint := func(context.Context) (WorkerEndpoint, error) {
		return WorkerEndpoint{Capabilities: append(append([]string{}, boundedWorkerRequired...), workerRequired...), Origin: server.URL, StoreID: "store", InstanceID: "instance", PrincipalID: "owner", Execution: api.WorkExecutionSettings{Model: "fixture"}, ModelConfigured: true, ModelAuth: "unknown", Enroll: func(_ context.Context, _ string, token string) (wire.ApplicationConnection, error) {
			enrolls.Add(1)
			secret = token
			return wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "connection", PrincipalId: "owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}}, nil
	}
	options := WorkerOptions{Protocol: WorkerProtocolBoundedApplication, Directory: directory, Target: target, Source: fixtureWorkSource{source}, Endpoint: endpoint}
	client := NewWorker(options)
	if err := client.engine.connectWorker(ctx); err != nil {
		t.Fatal(err)
	}
	client.engine.connected = true
	in := api.WorkStart{ID: "task", Workspace: "/remote/target-workspace", Instructions: "bounded task only", Source: source, RequestDigest: digest([]byte("bounded intent")), TaskStart: api.TaskStart{RequestID: "request", Title: "task", Prompt: "task", Target: &target}}
	if _, err := client.StartWork(ctx, in); err != nil {
		t.Fatal(err)
	}
	if client.engine.state.Session.SessionId != "" || client.engine.state.Workers["task"].Native {
		t.Fatal("bounded task invented resident or shared-native identity")
	}
	changed := in
	changed.Instructions = "changed worker instruction"
	if _, err := client.StartWork(ctx, changed); err == nil {
		t.Fatal("recorded bounded profile instructions changed")
	}
	restored := NewWorker(options)
	if err := restored.engine.connectWorker(ctx); err != nil || creates.Load() != 1 || enrolls.Load() != 1 {
		t.Fatal("bounded reconnect replaced original native binding", err)
	}
	// Canonical terminal status never acknowledges a missing generic action
	// receipt, and another task's unknown mutation cannot taint this one.
	client.engine.state.Views["bounded-task"] = &view{State: wire.SessionState{SessionId: "bounded-task", Run: wire.RunState{Status: pointer("completed")}}}
	client.engine.state.Operations["other"] = journal{Path: "/sessions/other/cancel", Outcome: "unknown"}
	observed, err := client.ReadWork(ctx, "task")
	if err != nil || observed.Status != "completed" || observed.Outcome == "unknown" {
		t.Fatal("unrelated action changed bounded task", err)
	}
	for _, path := range []string{"/sessions/bounded-task/cancel", "/sessions/bounded-task/approvals/native/resolve"} {
		client.engine.state.Operations["original-action"] = journal{Path: path, Outcome: "unknown"}
		observed, err = client.ReadWork(ctx, "task")
		if err != nil || observed.Status != "completed" || observed.Outcome != "unknown" || client.engine.state.Operations["original-action"].Outcome != "unknown" {
			t.Fatal("native completion falsely acknowledged original action", err)
		}
	}
	wrong := options
	wrong.Protocol = WorkerProtocolSharedNative
	if err := NewWorker(wrong).engine.connectWorker(ctx); err == nil || creates.Load() != 1 || enrolls.Load() != 1 {
		t.Fatal("protocol changed an existing scoped credential")
	}
	corrupted.Store(true)
	if err := NewWorker(options).engine.connectWorker(ctx); err == nil || creates.Load() != 1 {
		t.Fatal("bounded immutable creation digest changed")
	}
}
