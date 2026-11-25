package caelis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/testutil"
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
	s.state.Session = wire.SessionBinding{PrincipalId: "owner", SessionId: "main"}
	s.state.PrincipalID = "owner"
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

func TestComputerUseApprovalPreservesEveryNativeOptionAndSingleResolution(t *testing.T) {
	a := testApproval()
	a.RequestId = "computer-use-request"
	a.Permission = map[string]any{"tool_call": map[string]any{"id": "cua-repl", "name": "cua_repl", "title": "Allow Computer Use", "raw_input": map[string]any{"fixture": true}}, "options": []any{
		map[string]any{"id": "once", "name": "Allow once", "kind": "allow_once"},
		map[string]any{"id": "session", "name": "Allow this session", "kind": "allow_always"},
		map[string]any{"id": "always", "name": "Always allow", "kind": "allow_always"},
		map[string]any{"id": "deny", "name": "Deny", "kind": "reject_once"},
	}}
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeFixture(w, wire.SessionState{SessionId: "main", Approval: wire.ApprovalState{Active: a}})
			return
		}
		posts.Add(1)
		var req wire.ResolveApprovalRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		chosen := value(req.OptionId)
		approved := chosen == "session" || chosen == "always"
		if req.ApprovalRequestId != a.RequestId || chosen != "session" && chosen != "always" && chosen != "deny" || req.Approved != approved || req.Target != *a.Target {
			t.Error("native Computer Use decision lost exact option or target")
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed"})
	})
	s.state.Views["main"].State.Approval.Active = a
	approval := s.Snapshot().Approvals[0]
	for i, want := range []string{"once", "session", "always", "deny"} {
		if approval.Choices[i].ID != want || approval.Choices[i].Label == "" {
			t.Fatalf("native choice %d lost: %+v", i, approval.Choices)
		}
	}
	if err := s.Decide(t.Context(), api.Decision{ID: approval.ID, Choice: "invented-forever"}); err == nil || posts.Load() != 0 {
		t.Fatal("unoffered persistent permission was accepted")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, option := range []string{"session", "always"} {
		go func(option string) {
			<-start
			results <- s.Decide(t.Context(), api.Decision{ID: approval.ID, Choice: option})
		}(option)
	}
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) || posts.Load() != 1 {
		t.Fatal("competing clients resolved more than one native option", first, second, posts.Load())
	}
	a = clone(a)
	a.RequestId = "computer-use-deny"
	s.state.Views["main"].State.Approval.Active = a
	denial := s.Snapshot().Approvals[0]
	if err := s.Decide(t.Context(), api.Decision{ID: denial.ID, Choice: "deny"}); err != nil || posts.Load() != 2 {
		t.Fatal("native denial was not passed as an unapproved selection", err)
	}
}
func TestReplacementIsAtomicAndCursorIsOpaque(t *testing.T) {
	const oldCursor, oldHistory = "opaque+/=", "previous-history"
	original := commandResultEvidence{TurnID: "old-turn", Handle: "original-handle", TurnEnded: true}
	for _, tc := range []struct {
		name    string
		endPage *int
		synced  bool
	}{
		{name: "complete", endPage: pointer(1), synced: true},
		{name: "wrong_end_page", endPage: pointer(2)},
		{name: "eof_before_end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var feed strings.Builder
			send := func(event string, v any) {
				t.Helper()
				b, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&feed, "event: %s\ndata: %s\n\n", event, b)
			}
			send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
			send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})
			send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("s")})
			raw := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"new"}}`)
			send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("s"), Events: []wire.Envelope{{TurnId: pointer("turn"), Update: &raw}}})
			if tc.endPage != nil {
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("s"), Page: tc.endPage})
				// A rejected end must stop processing before this sync.
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", NextCursor: pointer("next"), HistoryBefore: pointer("older")})
			}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if r.Method != http.MethodGet || r.URL.Path != "/api/control/v1/sessions/main/reconnect" ||
					query.Get("after") != oldCursor || query.Get("history_turns") != "1" {
					t.Errorf("unexpected reconnect request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if _, err := io.WriteString(w, feed.String()); err != nil {
					t.Errorf("write fixture feed: %v", err)
				}
			})
			old := s.state.Views["main"]
			old.Items = []api.Item{{ID: "old", Text: "old"}}
			old.Cursor, old.HistoryBefore = oldCursor, oldHistory
			old.CommandResults = map[string]commandResultEvidence{"original": original}

			// The fixture closes after the last frame. Reading EOF requires all
			// preceding deliveries, including their checkpoint writes, to finish.
			err := s.watch(t.Context(), s.client, "main", "instance")
			if tc.endPage != nil && !tc.synced {
				if err == nil || err.Error() != "恢复快照不匹配" {
					t.Fatalf("watch error = %v, want replacement page mismatch", err)
				}
			} else if err != io.EOF {
				t.Fatalf("watch error = %v, want EOF", err)
			}
			got := s.state.Views["main"]
			if got.CommandCaughtUp != tc.synced {
				t.Fatalf("command followup caught up = %t, want %t", got.CommandCaughtUp, tc.synced)
			}
			if (got != old) != tc.synced {
				t.Fatalf("view replacement = %t, want %t", got != old, tc.synced)
			}
			text, cursor, history := "old", oldCursor, oldHistory
			if tc.synced {
				text, cursor, history = "new", "next", "older"
			}
			assertView := func(label string, v *view) {
				t.Helper()
				if v == nil {
					t.Fatalf("%s: view missing", label)
				}
				if len(v.Items) != 1 || v.Items[0].Text != text || v.Cursor != cursor ||
					v.HistoryBefore != history || v.CommandResults["original"] != original {
					t.Fatalf("%s: items=%+v cursor=%q history=%q command=%+v", label, v.Items, v.Cursor, v.HistoryBefore, v.CommandResults["original"])
				}
			}
			assertView("in memory", got)
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			assertView("checkpoint", restored.Views["main"])
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
	if e := testutil.SetMode(root, 0700); e != nil {
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
	if e := testutil.Redirect(secret, link); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(link, 1024); e == nil {
		t.Fatal("symlink credential accepted")
	}
	if e := testutil.SetMode(secret, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(secret, 1024); e == nil {
		t.Fatal("readable credential accepted")
	}
	if e := testutil.SetMode(secret, 0600); e != nil {
		t.Fatal(e)
	}
	if e := testutil.SetMode(root, 0755); e != nil {
		t.Fatal(e)
	}
	if _, e := privateRead(secret, 1024); e == nil {
		t.Fatal("unprotected directory accepted")
	}
	if e := testutil.SetMode(root, 0700); e != nil {
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
