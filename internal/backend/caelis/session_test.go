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
	s.state.Bot = wire.Bot{Id: "bot", SessionId: "main"}
	s.state.Client = wire.BotClient{Id: "client", BotId: "bot", ActivationId: "activation"}
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
func TestUnknownPromptSurvivesRestartWithoutReplay(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer SCOPED_TEST_TOKEN" {
			t.Error("wrong credential")
		}
		if r.Method == "POST" {
			posts.Add(1)
			drop(w)
			return
		}
		writeFixture(w, wire.BotRequestSource{Id: "source", BotId: "bot", ClientId: pointer("client"), OperationId: "original", Execution: wire.BotExecution{InstanceId: "instance", SessionId: "main", HandleId: "handle", RunId: "run", TurnId: "turn"}})
	})
	// Prime a reusable HTTP connection: even with Idempotency-Key, a lost
	// mutation response must not trigger net/http's automatic POST retry.
	if e := s.client.json(t.Context(), "GET", "/prime", nil, nil, "", ""); e != nil {
		t.Fatal(e)
	}
	in := api.Submission{ID: "original", Text: "one prompt"}
	receipt, e := s.Submit(t.Context(), in, nil)
	if e != nil || receipt.Outcome != "unknown" {
		t.Fatal(receipt, e)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	if restored.loadErr != nil {
		t.Fatal(restored.loadErr)
	}
	restored.client = s.client
	restored.connected = true
	for range 2 {
		receipt, e = restored.Submit(t.Context(), in, nil)
		if e != nil || receipt.Outcome != "unknown" {
			t.Fatal(receipt, e)
		}
	}
	other, _ := restored.Submit(t.Context(), api.Submission{ID: "replacement", Text: in.Text}, nil)
	if other.Outcome != "rejected" || posts.Load() != 1 {
		t.Fatal("unknown prompt was redispatched")
	}
	if e = restored.recoverPrompts(t.Context()); e != nil {
		t.Fatal(e)
	}
	receipt, e = restored.Submit(t.Context(), in, nil)
	if e != nil || receipt.Outcome != "accepted" || posts.Load() != 1 {
		t.Fatal("confirmed receipt replayed", receipt, e)
	}
	_, e = restored.command(t.Context(), in.ID, "/sessions/main/prompt", wire.PromptRequest{OperationId: &in.ID, Input: pointer("changed")})
	if e == nil {
		t.Fatal("conflicting request ID accepted")
	}
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
func TestDesktopUnknownClaimAndDurableReceiptNeverRepeatEffect(t *testing.T) {
	for _, loseClaim := range []bool{true, false} {
		t.Run(fmt.Sprint(loseClaim), func(t *testing.T) {
			var claims, receipts, effects atomic.Int32
			call := wire.BotDesktopCall{Id: "action", BotId: "bot", ClientId: "client", ActivationId: "activation", Action: "gesture", State: "queued"}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path[len(r.URL.Path)-5:] == "claim" {
					claims.Add(1)
					if loseClaim {
						drop(w)
						return
					}
					writeFixture(w, wire.BotDesktopClaim{Call: call, Token: "DISPATCH_SECRET"})
					return
				}
				receipts.Add(1)
				if receipts.Load() == 1 {
					drop(w)
					return
				}
				writeFixture(w, call)
			})
			s.effects.Execute = func(string, json.RawMessage) (json.RawMessage, error) {
				effects.Add(1)
				return json.RawMessage(`{"ok":true}`), nil
			}
			if e := s.dispatch(t.Context(), call); e == nil {
				t.Fatal("lost reply should be uncertain")
			}
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.client = s.client
			restored.effects = s.effects
			if restored.loadErr != nil {
				t.Fatal(restored.loadErr)
			}
			if e := restored.dispatch(t.Context(), call); e != nil {
				t.Fatal(e)
			}
			if claims.Load() != 1 || (loseClaim && effects.Load() != 0) || (!loseClaim && (effects.Load() != 1 || receipts.Load() != 2)) {
				t.Fatal("action repeated", claims.Load(), receipts.Load(), effects.Load())
			}
			public, _ := json.Marshal(restored.Snapshot())
			if strings.Contains(string(public), "DISPATCH_SECRET") {
				t.Fatal("token escaped to UI")
			}
		})
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

func TestUnknownReminderReconcilesQueueAdmissionWithoutReplay(t *testing.T) {
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { posts.Add(1); drop(w) })
	op := "fire-once"
	due := time.Now().UTC()
	req := wire.BotReminderRequest{BotId: "bot", GrantId: "grant", Version: "v1", Due: due, OperationId: &op}
	_, _ = s.command(t.Context(), op, s.botPath("/client/reminders/fire"), req)
	occurrence := wire.BotReminderFire{Id: "occurrence", BotId: "bot", ClientId: "client", GrantId: "grant", Version: "v1", Due: due, State: "claimed"}
	wrong := occurrence
	wrong.Version = "v2"
	if e := s.reconcileReminderReceipts([]wire.BotReminderFire{wrong}); e != nil {
		t.Fatal(e)
	}
	if s.state.Operations[op].Outcome != "unknown" {
		t.Fatal("unrelated occurrence resolved a command")
	}
	if e := s.reconcileReminderReceipts([]wire.BotReminderFire{occurrence}); e != nil {
		t.Fatal(e)
	}
	out, e := s.command(t.Context(), op, s.botPath("/client/reminders/fire"), req)
	if e != nil || !succeeded(out.Outcome) || posts.Load() != 1 || !s.Snapshot().CanSend {
		t.Fatal("queue admission not reconciled safely", out, e)
	}
}

func TestPollingCannotOverwriteNewerStreamState(t *testing.T) {
	var s *Session
	s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
		case "/api/control/v1/sessions/main/state":
			s.mu.Lock()
			v := s.state.Views["main"]
			v.State.Run = wire.RunState{Active: pointer(false), TurnId: pointer("finished"), Status: pointer("completed")}
			v.Observed++
			s.mu.Unlock()
			writeFixture(w, wire.SessionState{SessionId: "main", Run: wire.RunState{Active: pointer(true), TurnId: pointer("old")}})
		case "/api/control/v1/bots/bot/client/actions":
			writeFixture(w, wire.BotDesktopSnapshot{})
		default:
			writeFixture(w, []any{})
		}
	})
	s.state.StoreID = "store"
	s.state.Client.ExpiresAt = time.Now().Add(time.Hour)
	if e := s.refresh(t.Context()); e != nil {
		t.Fatal(e)
	}
	if snap := s.Snapshot(); !snap.CanSend || snap.CurrentTurn != "finished" {
		t.Fatal("stale HTTP state replaced a stream terminal fact", snap.Phase)
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

func TestConfigurationEventsStayOutOfChat(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	raw := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Bot settings saved by the user."}}`)
	config := wire.Envelope{EventId: pointer("bot-config-save"), Scope: pointer("main"), Update: &raw}
	applyEnvelope(v, config)
	applyEnvelope(v, config) // A reconnect must not surface the configuration either.
	if len(v.Items) != 0 || !v.Seen["bot-config-save"] {
		t.Fatal("configuration became a chat message or was not deduplicated")
	}
	// Identical user prose, and an unrelated out-of-turn event, remain visible.
	applyEnvelope(v, wire.Envelope{EventId: pointer("user-send"), TurnId: pointer("turn"), Update: &raw})
	applyEnvelope(v, wire.Envelope{EventId: pointer("other-event"), Update: &raw})
	if len(v.Items) != 2 || v.Items[0].Kind != "user" || v.Items[0].Text != "Bot settings saved by the user." {
		t.Fatal("presentation filter swallowed real conversation")
	}
}

func TestProjectionUpgradePreservesNativeJournals(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "binding.json")
	b, err := loadBinding(path)
	if err != nil {
		t.Fatal(err)
	}
	b.ProjectionVersion = 0
	b.Bot.Id = "same-bot"
	b.Operations["pending"] = journal{Outcome: "unknown", Digest: "same-digest"}
	b.Actions["effect"] = actionRecord{Phase: "receipted"}
	b.Views["main"] = &view{Items: []api.Item{{Text: "old projection"}}, Cursor: "old-cursor", Seen: map[string]bool{"old-event": true}}
	if err = privateWrite(path, b); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(path)
	if err != nil {
		t.Fatal(err)
	}
	v := restored.Views["main"]
	if restored.ProjectionVersion != currentProjectionVersion || len(v.Items) != 0 || v.Cursor != "" || len(v.Seen) != 0 {
		t.Fatal("stale projection was not reset for replay")
	}
	if restored.Bot.Id != b.Bot.Id || restored.Operations["pending"].Outcome != "unknown" || restored.Operations["pending"].Digest != "same-digest" || restored.Actions["effect"].Phase != "receipted" {
		t.Fatal("projection upgrade changed native identity or operation journal")
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

func TestAcceptedEnrollmentSurvivesLostBotRead(t *testing.T) {
	var creates, registrations atomic.Int32
	var lost atomic.Bool
	bot := wire.Bot{Id: "created", SessionId: "main", Config: wire.BotConfig{ManagedWork: pointer(true), DesktopActions: pointer(true)}}
	life := wire.BotClient{Id: "client", BotId: bot.Id, PrincipalId: "principal", InstanceId: "instance", Active: true, ActivationId: "activation", ExpiresAt: time.Now().Add(time.Hour)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/control/v1/initialize":
			writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: required})
		case "/api/control/v1/bots/create":
			if r.Header.Get("Authorization") != "Bearer HOST_SENTINEL" {
				t.Error("enrollment credential")
			}
			creates.Add(1)
			writeFixture(w, wire.CommandResult{OperationId: r.Header.Get("Idempotency-Key"), Outcome: "committed", Resource: &wire.CommandResource{Ref: &bot.Id}})
		case "/api/control/v1/bots/created/clients/register":
			if r.Header.Get("Authorization") != "Bearer HOST_SENTINEL" {
				t.Error("registration credential")
			}
			registrations.Add(1)
			writeFixture(w, wire.BotClientRegistration{Client: life, Token: "SCOPED_SENTINEL"})
		case "/api/control/v1/bots/created/client":
			if r.Header.Get("Authorization") != "Bearer SCOPED_SENTINEL" {
				t.Error("Host bearer escaped into daily request")
			}
			writeFixture(w, life)
		case "/api/control/v1/bots/created":
			if !lost.Load() {
				drop(w)
				return
			}
			writeFixture(w, bot)
		default:
			t.Error("unexpected route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	store := filepath.Join(root, "store")
	path := filepath.Join(store, "runtime/service/discovery.json")
	if e := privateWrite(path, discovery{Schema: "caelis.control.service-discovery/v1", Endpoint: server.URL, InstanceID: "instance", PrincipalID: "principal"}); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(filepath.Dir(path), "auth.token"), []byte("HOST_SENTINEL"), 0600); e != nil {
		t.Fatal(e)
	}
	opts := Options{Directory: filepath.Join(root, "bot"), Settings: api.RuntimeSettings{Runtime: "caelis", CaelisStore: store}}
	s := New(opts)
	if e := s.connect(t.Context()); e == nil {
		t.Fatal("expected lost read")
	}
	lost.Store(true)
	restored := New(opts)
	if e := restored.connect(t.Context()); e != nil {
		t.Fatal(e)
	}
	if creates.Load() != 1 || registrations.Load() != 1 || restored.state.Bot.SessionId != "main" {
		t.Fatal("enrollment was repeated")
	}
	raw, e := os.ReadFile(restored.path)
	if e != nil || strings.Contains(string(raw), "SENTINEL") {
		t.Fatal("binding contains bearer")
	}
}
