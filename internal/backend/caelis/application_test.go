package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestConfigurationLostResponseKeepsExactReceiptAndExplicitClear(t *testing.T) {
	var posts atomic.Int32
	receipt := wire.ApplicationConfiguration{SessionId: "main", Revision: "2", Profile: wire.ApplicationProfile{Instructions: ""}}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			var patch map[string]json.RawMessage
			_ = json.Unmarshal(body["patch"], &patch)
			if string(patch["tools"]) != "[]" || string(patch["native_tools"]) != "[]" || string(patch["instructions"]) != `""` {
				t.Error("explicit clear omitted")
			}
			drop(w)
			return
		}
		if strings.Contains(r.URL.Path, "configuration-operations") {
			writeFixture(w, receipt)
			return
		}
		latest := receipt
		latest.Revision = "3"
		latest.Profile.Instructions = "later"
		writeFixture(w, latest)
	})
	patch := map[string]any{"instructions": "", "tools": []any{}, "native_tools": []string{}}
	if _, e := s.UpdateConfiguration(t.Context(), "lost-update", "1", patch); e == nil {
		t.Fatal("lost response should remain uncertain")
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client = s.client
	out, e := restored.UpdateConfiguration(t.Context(), "lost-update", "1", patch)
	if e != nil || out.Revision != "2" || posts.Load() != 1 || restored.state.Configurations["main"].Revision != "3" {
		t.Fatal("receipt replay/desired state corrupted", out, e)
	}
	if len(restored.state.Typed["lost-update"].Body) != 0 || restored.state.Typed["lost-update"].Digest == "" {
		t.Fatal("confirmed intent retained payload or lost fingerprint")
	}
	if _, e = restored.UpdateConfiguration(t.Context(), "lost-update", "1", map[string]any{"instructions": "different"}); e == nil {
		t.Fatal("changed retry accepted")
	}
}
func TestUnknownApplicationPromptIsReadNotRedispatched(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			drop(w)
			return
		}
		writeFixture(w, wire.ApplicationOperation{OperationId: "original", Outcome: "accepted", Result: &wire.CommandResult{OperationId: "original", Outcome: "accepted"}})
	})
	in := api.Submission{ID: "original", Text: "synthetic message"}
	receipt, e := s.Submit(t.Context(), in, nil)
	if e != nil || receipt.Outcome != "unknown" {
		t.Fatal(receipt, e)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client = s.client
	restored.connected = true
	for range 2 {
		r, e := restored.Submit(t.Context(), in, nil)
		if e != nil || r.Outcome != "unknown" {
			t.Fatal(r, e)
		}
	}
	if e = restored.recoverOperations(t.Context()); e != nil {
		t.Fatal(e)
	}
	r, e := restored.Submit(t.Context(), in, nil)
	if e != nil || r.Outcome != "accepted" || posts.Load() != 1 {
		t.Fatal("unknown prompt replayed", r, e)
	}
}
func TestCallbacksLostClaimOrResultNeverRepeatEffect(t *testing.T) {
	for _, loss := range []string{"claim", "result"} {
		t.Run(loss, func(t *testing.T) {
			var effects, claims, results atomic.Int32
			call := wire.ApplicationCall{Id: "opaque", CallId: "reusable-provider-id", SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", TurnId: "turn", ItemId: "item", Name: "FixtureLookup", ToolsVersion: "v1", ConfigurationRevision: "1", Source: wire.ApplicationSource{Kind: "user", OperationId: "prompt"}, State: "pending", Arguments: map[string]any{"key": "fixture"}}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/claim") {
					claims.Add(1)
					if loss == "claim" {
						drop(w)
						return
					}
					claimed := call
					claimed.State = "claimed"
					writeFixture(w, claimed)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/result") {
					count := results.Add(1)
					var result wire.ApplicationCallResult
					_ = json.NewDecoder(r.Body).Decode(&result)
					want := "succeeded"
					if loss == "claim" {
						want = "unknown"
					}
					if result.Outcome != want {
						t.Error("wrong uncertainty", result.Outcome)
					}
					if loss == "result" && count == 1 {
						drop(w)
						return
					}
					writeFixture(w, struct{}{})
					return
				}
				t.Error("unexpected route")
			})
			h := &acceptanceTools{call: func(context.Context, string, json.RawMessage) api.ToolResult {
				effects.Add(1)
				return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "effect"}}}
			}}
			s.catalogs = map[string]map[string]api.ApplicationTools{"v1": {"FixtureLookup": h}}
			s.state.Operations["prompt"] = journal{Path: "/application/sessions/main/prompt", Outcome: "accepted", Source: call.Source}
			_ = s.handleCall(t.Context(), s.client, s.state.Session, s.state.Connection, call)
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.catalogs = s.catalogs
			restored.client = s.client
			call.State = "claimed"
			if e := restored.handleCall(t.Context(), s.client, s.state.Session, s.state.Connection, call); e != nil {
				t.Fatal(e)
			}
			want := int32(1)
			if loss == "claim" {
				want = 0
			}
			if effects.Load() != want || claims.Load() != 1 {
				t.Fatal("effect/claim repeated", effects.Load(), claims.Load())
			}
			call.ItemId = "forged"
			if e := restored.handleCall(t.Context(), s.client, s.state.Session, s.state.Connection, call); e == nil {
				t.Fatal("changed invocation accepted")
			}
		})
	}
}
func TestResourceDigestMismatchNeverReturnsBytes(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, wire.ApplicationResourceContent{Data: "eA==", Resource: wire.ApplicationResource{Id: "r", SessionId: "main", Size: 1, Sha256: "wrong"}})
	})
	b, _, e := s.ReadResource(t.Context(), "main", "r")
	if e == nil || b != nil {
		t.Fatal("unverified bytes exposed")
	}
}
func TestSourceEvidenceCannotAuthorizeWorkOrBackground(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("untrusted source reached Host") })
	s.state.PrincipalID = "owner"
	for _, kind := range []string{"application_summary", "external_material", ""} {
		ctx := context.WithValue(t.Context(), invocationKey{}, wire.ApplicationCall{SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Source: wire.ApplicationSource{Kind: kind}})
		if s.WorkAdmission(ctx) == nil {
			t.Fatal("source gained delegation authority", kind)
		}
		if s.AuthorizeBackground(ctx, "reminder", "fingerprint") == nil {
			t.Fatal("source gained background authority", kind)
		}
	}
}

func TestNativeArtifactProjectionAcceptsToolContentUnion(t *testing.T) {
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
	result, _ := json.Marshal(map[string]any{"resource": wire.ApplicationResource{Id: "artifact", SessionId: "main", Name: "result.txt"}})
	for _, name := range []string{"ReadResource", "PublishArtifact", "PublishArtifact"} {
		raw, _ := json.Marshal(map[string]any{"sessionUpdate": "tool_call_update", "name": name, "toolCallId": "call", "status": "completed", "content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "text", "text": string(result)}}}, "rawOutput": map[string]string{"result": string(result)}})
		update := json.RawMessage(raw)
		applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &update})
	}
	if len(v.Items) != 1 || len(v.Items[0].Artifacts) != 1 || v.Items[0].Artifacts[0].ID != "resource:main:artifact" {
		t.Fatal("artifact missing or duplicated", v.Items)
	}
}

func TestWorkerStartRecoversLostCreateAndPromptWithoutRedispatch(t *testing.T) {
	var creates, prompts, grants atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case r.Method == "POST" && path == "/application/workers":
			creates.Add(1)
			var in wire.CreateWorkerRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			if value(in.Model) != "fixture-model" || value(in.ReasoningEffort) != "high" || !value(in.FastMode) {
				t.Error("worker lost independent model")
			}
			drop(w)
		case r.Method == "POST" && strings.HasSuffix(path, "/prompt"):
			prompts.Add(1)
			drop(w)
		case r.Method == "POST" && strings.HasSuffix(path, "/background-grants"):
			grants.Add(1)
			var in wire.ApplicationBackgroundGrantRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			writeFixture(w, wire.ApplicationBackgroundGrant{Id: "grant", SessionId: "worker", Source: in.Source, AuthorizationOperationId: in.AuthorizationOperationId})
		case strings.Contains(path, "/application/operations/"):
			op := filepath.Base(path)
			writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: "accepted", Result: &wire.CommandResult{OperationId: op, Outcome: "accepted", SessionId: pointer("worker")}})
		case path == "/application/workers":
			writeFixture(w, []wire.ApplicationWorker{{SessionId: "worker", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner"}})
		default:
			t.Error("unexpected worker recovery route", r.Method, path)
			w.WriteHeader(404)
		}
	})
	s.workExecution = api.WorkExecutionSettings{Model: "fixture-model", Effort: "high", ServiceTier: "priority"}
	s.state.PrincipalID = "owner"
	s.state.Configurations["main"] = wire.ApplicationConfiguration{Profile: wire.ApplicationProfile{Model: "bot-luna", Execution: "workspace-write"}}
	ctx := context.WithValue(t.Context(), invocationKey{}, wire.ApplicationCall{SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Source: wire.ApplicationSource{Kind: "user", OperationId: "authorized-user"}})
	_, _ = s.StartWork(ctx, api.WorkStart{ID: "job", TaskStart: api.TaskStart{RequestID: "request", Title: "Fixture", Prompt: "synthetic"}, Workspace: t.TempDir()})
	for range 2 {
		restored := New(Options{Directory: filepath.Dir(s.path), WorkExecution: api.WorkExecutionSettings{Model: "changed-setting"}})
		if pending := restored.state.Workers["job"].Start; pending != nil && pending.Profile.Model != "fixture-model" {
			t.Fatal("restart changed pending work model")
		}
		restored.client = s.client
		s = restored
		if e := s.recoverOperations(t.Context()); e != nil {
			t.Fatal(e)
		}
		_, _ = s.advanceWorker(t.Context(), s.state.Workers["job"])
	}
	worker := s.state.Workers["job"]
	if creates.Load() != 1 || prompts.Load() != 1 || grants.Load() != 0 || worker.Binding.SessionId != "worker" || worker.Start != nil || worker.Task.Outcome != "accepted" {
		t.Fatal("worker recovery lost identity or repeated dispatch", creates.Load(), prompts.Load(), grants.Load(), worker)
	}
}
