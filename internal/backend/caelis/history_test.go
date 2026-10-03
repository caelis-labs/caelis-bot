package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func historyMessage(id, turn, text string) wire.Envelope {
	raw, _ := json.Marshal(map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": id, "content": map[string]string{"type": "text", "text": text}})
	u := json.RawMessage(raw)
	return wire.Envelope{Kind: "session/update", EventId: pointer(id), TurnId: pointer(turn), Final: pointer(true), Update: &u}
}

func historyFrames(w http.ResponseWriter, deliveries ...wire.SessionFeedDelivery) {
	w.Header().Set("Content-Type", "text/event-stream")
	b, _ := json.Marshal(wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{Active: pointer(false)}})
	fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
	for _, d := range deliveries {
		b, _ = json.Marshal(d)
		fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
	}
}

func TestHistoryPrependsBothSourcesWithoutReplayingAuthority(t *testing.T) {
	for _, replacement := range []bool{true, false} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Query().Get("history_turns") != "8" || r.URL.Query().Get("history_before") != "opaque+/=" || r.URL.Query().Has("after") || r.Header.Get("Last-Event-ID") != "" {
					t.Error("history used wrong traversal or mutation")
				}
				events := []wire.Envelope{historyMessage("older", "old-turn", "older"), historyMessage("overlap", "live-turn", "stale copy"), {Kind: "caelis/lifecycle", TurnId: pointer("old-turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}}, {Kind: "session/request_permission", ApprovalRequestId: pointer("old-approval")}}
				deliveries := []wire.SessionFeedDelivery{{Kind: "append_page", Source: "exact", NextCursor: pointer("page-cursor"), Events: events}}
				if replacement {
					deliveries = []wire.SessionFeedDelivery{{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("history")}, {Kind: "replace_page", Source: "replacement", SnapshotId: pointer("history"), Events: events}, {Kind: "replace_end", Source: "replacement", SnapshotId: pointer("history"), Page: pointer(1)}}
				}
				historyFrames(w, append(deliveries, wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})...)
			})
			v := s.state.Views["main"]
			v.HistoryBefore, v.Cursor, v.CommandCaughtUp = "opaque+/=", "live-cursor", true
			v.Items = []api.Item{{ID: "live-turn/assistant/overlap", TurnKey: "live-turn", Kind: "assistant", Text: "newer live text", Status: "inProgress"}}
			v.State.Run = wire.RunState{Active: pointer(true), TurnId: pointer("live-turn"), Status: pointer("running")}
			v.State.Approval.Active = &wire.ActiveApproval{RequestId: "live-approval"}
			v.CommandResults = map[string]commandResultEvidence{"command": {TurnID: "live-turn", Handle: "original"}}
			state, commands, operations := clone(v.State), clone(v.CommandResults), clone(s.state.Operations)
			if !s.Snapshot().HasEarlier {
				t.Fatal("pagination hidden")
			}
			if err := s.LoadEarlier(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(v.Items) != 2 || v.Items[0].Text != "older" || v.Items[1].Text != "newer live text" || s.Snapshot().HasEarlier {
				t.Fatal("older display did not prepend or preserve overlap")
			}
			if v.Cursor != "live-cursor" || !v.CommandCaughtUp || v.ApprovalDirty || !reflect.DeepEqual(state, v.State) || !reflect.DeepEqual(commands, v.CommandResults) || !reflect.DeepEqual(operations, s.state.Operations) {
				t.Fatal("history changed live authority")
			}
			loaded, err := loadBinding(s.path)
			if err != nil || loaded.Views["main"].HistoryBefore != "" || len(loaded.Views["main"].Items) != 2 {
				t.Fatal("history not checkpointed", err)
			}
		})
	}
}

func TestIncompleteHistoryKeepsItemsAndBoundary(t *testing.T) {
	for _, failure := range []string{"truncated", "wrong-page", "same-boundary", "live-cursor"} {
		t.Run(failure, func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				start := wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("page")}
				page := wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("page"), Events: []wire.Envelope{historyMessage("old", "old", "older")}}
				end := wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("page"), Page: pointer(1)}
				sync := wire.SessionFeedDelivery{Kind: "sync", Source: "exact"}
				if failure == "wrong-page" {
					end.Page = pointer(2)
				}
				if failure == "same-boundary" {
					sync.HistoryBefore = pointer("before")
				}
				if failure == "live-cursor" {
					sync.NextCursor = pointer("wrong-live-cursor")
				}
				if failure == "truncated" {
					historyFrames(w, start, page)
					return
				}
				historyFrames(w, start, page, end, sync)
			})
			v := s.state.Views["main"]
			v.HistoryBefore, v.Cursor = "before", "live"
			v.Items = []api.Item{{ID: "original", Text: "keep"}}
			before := clone(v)
			if s.LoadEarlier(t.Context()) == nil || !reflect.DeepEqual(before, v) || !s.connected {
				t.Fatal("invalid history changed current view")
			}
		})
	}
}

func TestHistoryReplacementDuringReadRejectsStalePage(t *testing.T) {
	var s *Session
	s = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.state.Views["main"] = &view{Items: []api.Item{{ID: "new", Text: "replaced"}}, HistoryBefore: "new-before"}
		s.mu.Unlock()
		historyFrames(w, wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{historyMessage("old", "old", "stale")}}, wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})
	})
	s.state.Views["main"].HistoryBefore = "before"
	if s.LoadEarlier(t.Context()) == nil || len(s.state.Views["main"].Items) != 1 || s.state.Views["main"].Items[0].Text != "replaced" {
		t.Fatal("stale history escaped")
	}
}

func TestExactResumeKeepsEarlierHistoryBoundary(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") != "live" {
			t.Error("resume cursor changed")
		}
		historyFrames(w, wire.SessionFeedDelivery{Kind: "sync", Source: "exact", NextCursor: pointer("next")})
	})
	v := s.state.Views["main"]
	v.Cursor, v.HistoryBefore = "live", "old-before"
	_ = s.watch(t.Context(), s.client, "main", "instance")
	if v.HistoryBefore != "old-before" || v.Cursor != "next" || !s.Snapshot().HasEarlier {
		t.Fatal("exact resume lost older history")
	}
}
