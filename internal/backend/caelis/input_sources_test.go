package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestPastSessionDoesNotAcquireCurrentInputScope(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected native request")
		http.NotFound(w, r)
	})
	s.state.PastSessions = []string{"old-session"}
	s.state.Views["old-session"] = &view{State: wire.SessionState{SessionId: "old-session"}, Seen: map[string]bool{}, Items: []api.Item{
		{ID: "old-input", Kind: "user", Status: "completed", TurnKey: "old-turn", RequestID: "old-wx-request", Text: "Fixture old request"},
	}}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	got := backend.NewService(s, nil, nil, nil, nil).Snapshot()
	for _, source := range got.InputSources {
		if source.RequestID == "old-wx-request" && source.RuntimeOwner == got.RuntimeOwner {
			t.Fatalf("restored old-session input received current session owner: %+v", source)
		}
	}
}

func TestNativeInputScopeSurvivesPastSessionCacheAndReload(t *testing.T) {
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Fixture input"}}`)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prompt") {
			var request wire.PromptRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			writeFixture(w, wire.CommandResult{OperationId: value(request.OperationId), Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("native-turn")}})
			return
		}
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/sessions/main/reconnect") {
			t.Error("unexpected route")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, data any) {
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{
			{EventId: pointer("native-input"), SessionId: pointer("main"), TurnId: pointer("native-turn"), Update: &user},
		}})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})
	})
	if receipt, err := s.Submit(t.Context(), api.Submission{ID: "native-request", Text: "Fixture input"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	_ = s.watch(t.Context(), s.client, "main", "instance") // finite canonical feed closes after sync
	before := backend.NewService(s, nil, nil, nil, nil).Snapshot()
	if len(before.InputSources) != 1 || before.InputSources[0].RequestID != "native-request" || before.InputSources[0].RuntimeOwner != before.RuntimeOwner {
		t.Fatal("scoped feed/receipt did not establish input", before.InputSources)
	}
	s.state.PastSessions = []string{"main"}
	s.state.Session.SessionId = "next-session"
	s.state.InstanceID = "next-instance"
	s.state.Views["next-session"] = &view{State: wire.SessionState{SessionId: "next-session"}, Seen: map[string]bool{}}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	after := backend.NewService(s, nil, nil, nil, nil).Snapshot()
	if before.RuntimeOwner == after.RuntimeOwner || !reflect.DeepEqual(before.InputSources, after.InputSources) || len(after.Items) != 1 {
		t.Fatal("native persisted history changed scope or disappeared")
	}
	s.mu.Lock()
	cached := s.Snapshot()
	s.mu.Unlock()
	if !reflect.DeepEqual(cached.InputSources, before.InputSources) {
		t.Fatal("cache lost input evidence")
	}
	cached.InputSources[0].RuntimeOwner = "mutated"
	if !reflect.DeepEqual(s.Snapshot().InputSources, before.InputSources) {
		t.Fatal("source cache alias")
	}
	encoded, err := json.Marshal(after)
	if err != nil || strings.Contains(string(encoded), "InputSources") || strings.Contains(string(encoded), before.RuntimeOwner) {
		t.Fatal("private source exposed to renderer")
	}
}

func TestInputScopeRejectsMissingAndMismatchedNativeEvidence(t *testing.T) {
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Fixture"}}`)
	for _, scope := range []inputScope{{}, {SessionID: "main"}, {InstanceID: "instance", SessionID: "other-session"}} {
		v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
		applyScopedEnvelope(v, wire.Envelope{EventId: pointer("input"), SessionId: pointer("main"), TurnId: pointer("turn"), InputOperationId: pointer("request"), Update: &user}, scope)
		if len(v.InputScopes) != 0 {
			t.Fatal("missing or mismatched native feed gained source")
		}
	}
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
	applyScopedEnvelope(v, wire.Envelope{EventId: pointer("input"), SessionId: pointer("other-session"), TurnId: pointer("turn"), InputOperationId: pointer("request"), Update: &user}, inputScope{InstanceID: "instance", SessionID: "main"})
	if len(v.InputScopes) != 0 {
		t.Fatal("contradictory envelope gained feed scope")
	}
}

func TestEarlierInputKeepsRequestedSessionScope(t *testing.T) {
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Fixture older input"}}`)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/sessions/old-session/reconnect") || r.URL.Query().Get("history_before") != "older" {
			t.Error("history read escaped original native session")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, data any) {
			b, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("caelis.control.bootstrap", wire.SessionState{SessionId: "old-session", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{{EventId: pointer("older-input"), SessionId: pointer("old-session"), TurnId: pointer("older-turn"), InputOperationId: pointer("older-request"), Update: &user}}})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer("")})
	})
	s.state.PastSessions = []string{"old-session"}
	s.state.Views["old-session"] = &view{State: wire.SessionState{SessionId: "old-session"}, Seen: map[string]bool{}, HistoryBefore: "older"}
	if err := s.LoadEarlier(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []api.InputSource{{RuntimeOwner: (inputScope{InstanceID: "instance", SessionID: "old-session"}).owner(), TurnKey: "older-turn", RequestID: "older-request"}}
	got := backend.NewService(s, nil, nil, nil, nil).Snapshot()
	if !reflect.DeepEqual(got.InputSources, want) || got.RuntimeOwner == want[0].RuntimeOwner || len(got.Items) != 1 {
		t.Fatal("older native page acquired current scope")
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	if !reflect.DeepEqual(s.Snapshot().InputSources, want) {
		t.Fatal("older-page input scope lost on reopen")
	}
}

func TestNativeReplayCannotUpgradeRetainedInputScope(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		for _, proven := range []bool{false, true} {
			t.Run(fmt.Sprintf("replacement_%t/proven_%t", replacement, proven), func(t *testing.T) {
				user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Fixture input"}}`)
				event := wire.Envelope{EventId: pointer("replayed-input"), SessionId: pointer("main"), TurnId: pointer("original-turn"), InputOperationId: pointer("original-request"), Update: &user}
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/sessions/main/reconnect") {
						t.Error("unexpected native request")
						http.NotFound(w, r)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					send := func(name string, data any) {
						b, _ := json.Marshal(data)
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
					}
					send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
					if replacement {
						for _, delivery := range []wire.SessionFeedDelivery{
							{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("replay")},
							{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("replay"), Events: []wire.Envelope{event}},
							{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("replay"), Page: pointer(1)},
						} {
							send("caelis.control.delivery", delivery)
						}
					} else {
						send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{event}})
					}
					send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})
				})
				id := "original-turn/user/original-request"
				v := s.state.Views["main"]
				v.Items = []api.Item{{ID: id, Kind: "user", TurnKey: "original-turn", RequestID: "original-request", Text: "Fixture original input"}}
				original := inputScope{InstanceID: "original-instance", SessionID: "main", TurnID: "original-turn"}
				if proven {
					v.InputScopes = map[string]inputScope{id: original}
				}
				if err := s.saveLocked(); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					restored, err := loadBinding(s.path)
					if err != nil {
						t.Fatal(err)
					}
					s.state = restored
					_ = s.watch(t.Context(), s.client, "main", "instance")
					got := backend.NewService(s, nil, nil, nil, nil).Snapshot()
					if len(got.Items) != 1 {
						t.Fatal("replay discarded historical input")
					}
					for _, source := range got.InputSources {
						if !proven || source.RuntimeOwner != original.owner() || source.RuntimeOwner == got.RuntimeOwner {
							t.Fatal("replay upgraded or relabeled retained input")
						}
					}
					if proven && len(got.InputSources) != 1 {
						t.Fatal("replay lost original proven input")
					}
				}
			})
		}
	}
}

func TestInputScopeSurvivesDisplayEvictionAndEarlierReload(t *testing.T) {
	for _, proven := range []bool{false, true} {
		t.Run(fmt.Sprintf("proven_%t", proven), func(t *testing.T) {
			user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Fixture old input"}}`)
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/sessions/main/reconnect") {
					t.Error("unexpected native request")
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(name string, data any) {
					b, _ := json.Marshal(data)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
				}
				send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
				if r.URL.Query().Get("history_before") == "older" {
					send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{{EventId: pointer("old-event"), SessionId: pointer("main"), TurnId: pointer("old-turn"), InputOperationId: pointer("old-request"), Update: &user}}})
					send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer("")})
					return
				}
				// The canonical recent window no longer includes the old input.
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("recent")})
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("recent")})
				send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer("older")})
			})
			id := "old-turn/user/old-request"
			original := inputScope{InstanceID: "old-instance", SessionID: "main", TurnID: "old-turn"}
			s.state.Views["main"].Items = []api.Item{{ID: id, Kind: "user", TurnKey: "old-turn", RequestID: "old-request", Text: "Fixture old input"}}
			if proven {
				s.state.Views["main"].InputScopes = map[string]inputScope{id: original}
			}
			_ = s.watch(t.Context(), s.client, "main", "instance")
			if len(s.Snapshot().Items) != 0 {
				t.Fatal("fixture did not evict old display item")
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			if err := s.LoadEarlier(t.Context()); err != nil {
				t.Fatal(err)
			}
			restored, err = loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			got := backend.NewService(s, nil, nil, nil, nil).Snapshot()
			if len(got.Items) != 1 {
				t.Fatal("fixture did not restore old display item")
			}
			if !proven {
				if len(got.InputSources) != 0 {
					t.Fatal("eviction/reload upgraded unproven history")
				}
			} else if len(got.InputSources) != 1 || got.InputSources[0].RuntimeOwner != original.owner() || got.InputSources[0].RuntimeOwner == got.RuntimeOwner {
				t.Fatal("eviction/reload lost original scope")
			}
		})
	}
}
