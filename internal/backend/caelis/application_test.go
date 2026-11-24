package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestConfigurationLostResponseKeepsExactReceiptAndExplicitClear(t *testing.T) {
	var posts atomic.Int32
	receipt := wire.SessionConfiguration{SessionId: "main", Revision: "2", Profile: &wire.SessionProfile{Instructions: ""}}
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
		if strings.Contains(r.URL.Path, "/operations/") {
			writeFixture(w, wire.CommandResult{OperationId: "lost-update", Outcome: "committed", Configuration: &receipt})
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
		writeFixture(w, &wire.CommandResult{OperationId: "original", Outcome: "accepted"})
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
func TestResourceDigestMismatchNeverReturnsBytes(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		writeFixture(w, wire.SessionResourceContent{Data: "eA==", Resource: wire.SessionResource{Id: "r", SessionId: "main", Size: 1, Sha256: "wrong"}})
	})
	b, _, e := s.ReadResource(t.Context(), "main", "r")
	if e == nil || b != nil {
		t.Fatal("unverified bytes exposed")
	}
}
func TestNativeArtifactProjectionAcceptsToolContentUnion(t *testing.T) {
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
	descriptor := map[string]any{"resource": wire.SessionResource{Id: "artifact", SessionId: "main", Name: "../private/result.txt"}}
	project := func(name string, output any) {
		raw, _ := json.Marshal(map[string]any{"sessionUpdate": "tool_call_update", "name": name,
			"toolCallId": "call", "status": "completed", "rawOutput": output})
		update := json.RawMessage(raw)
		applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &update})
	}
	project("ReadResource", descriptor)
	project("PublishArtifact", map[string]any{"resource": wire.SessionResource{Id: "other-resource", SessionId: "other-session", Name: "private.txt"}})
	project("PublishArtifact", map[string]string{"result": "malformed legacy text"})
	for _, item := range v.Items {
		if len(item.Artifacts) != 0 {
			t.Fatal("non-publication or wrong-owner resource reached the transcript", item)
		}
	}
	project("PublishArtifact", descriptor)
	project("PublishArtifact", descriptor)
	result, _ := json.Marshal(map[string]any{"resource": wire.SessionResource{Id: "legacy", SessionId: "main", Name: "../private/legacy.sql"}})
	project("PublishArtifact", map[string]string{"result": string(result)})
	want := []api.Artifact{{ID: "resource:main:artifact", Name: "result.txt"}, {ID: "resource:main:legacy", Name: "legacy.sql"}}
	var projected []api.Artifact
	for _, item := range v.Items {
		if len(item.Artifacts) > 0 && item.TurnKey != "turn" {
			t.Fatalf("artifact lost its native turn: %+v", item)
		}
		projected = append(projected, item.Artifacts...)
	}
	if !slices.Equal(projected, want) {
		t.Fatalf("invalid projected resources: %+v", projected)
	}
}

func TestWorkerStartRecoversLostCreateAndPromptWithoutRedispatch(t *testing.T) {
	var creates, prompts, grants atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case r.Method == "POST" && path == "/sessions":
			creates.Add(1)
			var in wire.CreateSessionRequest
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
			var in wire.CreateSessionBackgroundGrantRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			writeFixture(w, wire.SessionBackgroundGrant{Id: "grant", SessionId: "worker", Source: in.Source, AuthorizationOperationId: in.AuthorizationOperationId})
		case strings.Contains(path, "/operations/"):
			op := filepath.Base(path)
			writeFixture(w, &wire.CommandResult{OperationId: op, Outcome: "accepted", SessionId: pointer("worker")})
		case path == "/sessions/worker/state":
			writeFixture(w, wire.SessionState{SessionId: "worker"})
		default:
			t.Error("unexpected worker recovery route", r.Method, path)
			w.WriteHeader(404)
		}
	})
	s.workExecution = api.WorkExecutionSettings{Model: "fixture-model", Effort: "high", ServiceTier: "priority"}
	s.state.PrincipalID = "owner"
	s.state.Configurations["main"] = wire.SessionConfiguration{Profile: &wire.SessionProfile{Model: "bot-luna", Execution: "workspace-write"}}
	ctx := context.WithValue(t.Context(), workOwnerKey{}, s)
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
