package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func fixtureSession(t *testing.T, h http.HandlerFunc) *Session {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c, e := newClient(server.URL, "SCOPED_TEST_TOKEN")
	if e != nil {
		t.Fatal(e)
	}
	s := New(Options{Directory: filepath.Join(t.TempDir(), "private")})
	s.client = c
	s.connected = true
	s.state.Session = wire.ApplicationBinding{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", SessionId: "main"}
	s.state.Connection = wire.ApplicationConnection{ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner"}
	s.state.InstanceID = "instance"
	s.state.Views["main"] = &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}, Items: []api.Item{}}
	return s
}
func drop(w http.ResponseWriter) {
	c, _, e := w.(http.Hijacker).Hijack()
	if e == nil {
		c.Close()
	}
}
func writeFixture(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func TestHTTPOutcomeIsIndependentOfStatus(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		writeFixture(w, wire.CommandResult{OperationId: "conflict", Outcome: "conflicted"})
	})
	out, e := s.command(t.Context(), "conflict", "/operation", struct{}{})
	if e != nil || out.Outcome != "conflicted" || !s.Snapshot().CanSend {
		t.Fatal("known conflict became unknown", out, e)
	}
}
func testApproval() *wire.ActiveApproval {
	return &wire.ActiveApproval{RequestId: "approval", Target: &wire.TurnTarget{HandleId: "h", RunId: "r", TurnId: "t"}, Permission: map[string]any{"tool_call": map[string]any{"title": "Verify fixture", "raw_input": map[string]any{"command": "printf fixture"}}, "options": []any{map[string]any{"id": "native-allow", "name": "Allow once", "kind": "allow_once"}, map[string]any{"id": "native-reject", "name": "Reject once", "kind": "reject_once"}}}}
}
func TestApprovalUsesNativeChoicesAndRejectsChangedTarget(t *testing.T) {
	a := testApproval()
	var posts atomic.Int32
	var changed atomic.Bool
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			head := clone(a)
			if changed.Load() {
				head.Target.RunId = "other"
			}
			writeFixture(w, wire.SessionState{SessionId: "work", Approval: wire.ApprovalState{Active: head}})
			return
		}
		posts.Add(1)
		var req wire.ResolveApprovalRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Target != *a.Target || value(req.OptionId) != "native-allow" || !req.Approved {
			t.Error("lost native decision semantics")
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed"})
	})
	s.state.Views["work"] = &view{State: wire.SessionState{SessionId: "work", Approval: wire.ApprovalState{Active: a}}, Seen: map[string]bool{}}
	snap := s.Snapshot()
	if !snap.CanSend || len(snap.Approvals) != 1 || snap.Approvals[0].Choices[0].ID != "native-allow" || snap.Approvals[0].Details == "null" {
		t.Fatal("worker approval projection", snap.Approvals)
	}
	decision := api.Decision{ID: snap.Approvals[0].ID, Choice: "native-allow"}
	changed.Store(true)
	if e := s.Decide(t.Context(), decision); e == nil || posts.Load() != 0 {
		t.Fatal("stale approval dispatched")
	}
	changed.Store(false)
	if e := s.Decide(t.Context(), decision); e != nil || posts.Load() != 1 {
		t.Fatal(e)
	}
}
func TestReplacementIsAtomicAndCursorIsOpaque(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("after") != "opaque+/=" {
					t.Error("cursor was changed")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(event string, v any) {
					b, _ := json.Marshal(v)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
				}
				send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("s")})
				raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"new"}}`)
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("s"), Events: []wire.Envelope{{TurnId: pointer("turn"), Update: &raw}}})
				end := 1
				if !valid {
					end = 2
				}
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("s"), Page: &end})
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", NextCursor: pointer("next")})
			})
			s.state.Views["main"].Items = []api.Item{{ID: "old", Text: "old"}}
			s.state.Views["main"].Cursor = "opaque+/="
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_ = s.watch(ctx, s.client, "main", "instance")
			v := s.state.Views["main"]
			if valid {
				if len(v.Items) != 1 || v.Items[0].Text != "new" || v.Cursor != "next" {
					t.Fatal("replacement missing")
				}
			} else if v.Items[0].Text != "old" || v.Cursor != "opaque+/=" {
				t.Fatal("partial snapshot escaped")
			}
		})
	}
}
func TestApprovalSettlementDoesNotCompleteTurn(t *testing.T) {
	v := &view{State: wire.SessionState{Run: wire.RunState{Active: pointer(true)}}, Seen: map[string]bool{}}
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", ApprovalRequestId: pointer("decision"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	if !value(v.State.Run.Active) {
		t.Fatal("approval settlement ended its turn")
	}
}

func TestWorkerScopeDoesNotBecomeMainMessage(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"private worker"}}`)
	applyEnvelope(v, wire.Envelope{Scope: pointer("participant"), Update: &raw})
	if len(v.Items) != 0 {
		t.Fatal("worker text escaped into main conversation")
	}
}

func TestReportContextIsNotAHumanMessage(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	raw := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"same wording"}}`)
	source := &wire.ActorIdentity{Kind: pointer("system"), Id: pointer("bot-work-results")}
	applyEnvelope(v, wire.Envelope{EventId: pointer("context"), TurnId: pointer("report-turn"), AgentCommunicationSource: source, Update: &raw})
	if len(v.Items) != 0 {
		t.Fatal("Control report evidence appeared as a human request")
	}
	applyEnvelope(v, wire.Envelope{EventId: pointer("human"), TurnId: pointer("user-turn"), Update: &raw})
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"work result"}}`)
	applyEnvelope(v, wire.Envelope{EventId: pointer("reply"), TurnId: pointer("report-turn"), AgentCommunicationSource: source, Update: &reply})
	if len(v.Items) != 2 || v.Items[0].Kind != "user" || v.Items[1].Kind != "assistant" || v.Items[1].Text != "work result" {
		t.Fatal("real user message or assistant report was lost")
	}
}

func TestPrivateCredentialBoundary(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	secret := filepath.Join(root, "credential.json")
	if e := privateWrite(secret, map[string]string{"token": "PRIVATE_SENTINEL"}); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(secret, 1024); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "redirect.json")
	if e := os.Symlink(secret, link); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(link, 1024); e == nil {
		t.Fatal("symlink credential accepted")
	}
	if e := os.Chmod(secret, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(secret, 1024); e == nil {
		t.Fatal("readable credential accepted")
	}
	if e := os.Chmod(secret, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(root, 0755); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(secret, 1024); e == nil {
		t.Fatal("unprotected directory accepted")
	}
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
}

func TestHostOriginAndErrorsDoNotExposeCredential(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1", "http://example.com", "http://127.0.0.1?token=PRIVATE_SENTINEL", "http://user:password@127.0.0.1", "http://localhost"} {
		if _, e := newClient(origin, "PRIVATE_SENTINEL"); e == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		writeFixture(w, map[string]string{"code": "permission_denied", "error": "PRIVATE_SENTINEL"})
	})
	e := s.client.json(t.Context(), "GET", "/private", nil, nil, "", "")
	if e == nil || strings.Contains(e.Error(), "PRIVATE_SENTINEL") {
		t.Fatal("unsafe error projection")
	}
	raw, _ := json.Marshal(s.Snapshot())
	if strings.Contains(string(raw), "SCOPED_TEST_TOKEN") {
		t.Fatal("snapshot contains credential")
	}
}
