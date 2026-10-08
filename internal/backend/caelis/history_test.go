package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func historyMessage(id, turn, text string) wire.Envelope {
	raw, _ := json.Marshal(map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": id, "content": map[string]string{"type": "text", "text": text}})
	u := json.RawMessage(raw)
	return wire.Envelope{Kind: "session/update", EventId: pointer(id), TurnId: pointer(turn), Final: pointer(true), Update: &u}
}

func historyReview(event, turn, approval, call, status string) wire.Envelope {
	return wire.Envelope{Kind: "caelis/approval_review", EventId: pointer(event), SessionId: pointer("main"), TurnId: pointer(turn), ApprovalRequestId: pointer(approval), Delivery: wire.Delivery{Mode: wire.DeliveryModeMirror}, ApprovalReview: &wire.ApprovalReview{ItemId: pointer("item-" + turn), ToolCallId: pointer(call), ToolName: pointer("FixtureLookup"), Status: pointer(status), RawInput: wire.JSONObject{"turn": turn}}}
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
				events := []wire.Envelope{historyMessage("older", "old-turn", "older"), historyMessage("overlap", "live-turn", "stale copy"), historyReview("old-review", "old-turn", "old-approval", "provider-call", "denied"), historyReview("late-progress", "old-turn", "old-approval", "provider-call", "in_progress"), historyReview("old-failure", "old-turn", "failed-approval", "failed-call", "failed"), {Kind: "caelis/lifecycle", TurnId: pointer("old-turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}}, {Kind: "session/request_permission", ApprovalRequestId: pointer("old-approval")}}
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
			oldReviewID := reviewID("main", "old-turn", "old-approval")
			v.LiveReviews = map[string]reviewFact{oldReviewID: {Review: api.Review{ID: oldReviewID, Status: "inProgress"}, TurnID: "old-turn", ApprovalID: "old-approval"}}
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
			if len(v.Reviews) != 1 || v.Reviews[oldReviewID].Status != "denied" || len(v.LiveReviews) != 0 || len(s.Snapshot().Reviews) != 1 {
				t.Fatal("loaded terminal review did not replace stale progress without importing failed history", s.Snapshot().Reviews)
			}
			if v.Cursor != "live-cursor" || !v.CommandCaughtUp || v.ApprovalDirty || !reflect.DeepEqual(state, v.State) || !reflect.DeepEqual(commands, v.CommandResults) || !reflect.DeepEqual(operations, s.state.Operations) {
				t.Fatal("history changed live authority")
			}
			loaded, err := loadBinding(s.path)
			if err != nil || loaded.Views["main"].HistoryBefore != "" || len(loaded.Views["main"].Items) != 2 || len(loaded.Views["main"].Reviews) != 1 {
				t.Fatal("history not checkpointed", err)
			}
		})
	}
}

func TestOlderReviewsFollowOnlyRequestedPagesAcrossSeventeenTurns(t *testing.T) {
	var recent, older atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/reconnect") {
			t.Errorf("history caused a non-observation request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch before := r.URL.Query().Get("history_before"); before {
		case "":
			request := recent.Add(1)
			wantAfter := ""
			if request == 2 {
				wantAfter = "invalid-cursor"
			}
			if r.URL.Query().Get("history_turns") != "1" || r.URL.Query().Get("after") != wantAfter {
				t.Errorf("startup exceeded its recent one-Turn window: %s", r.URL)
			}
			historyFrames(w, wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("recent")}, wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("recent"), Events: []wire.Envelope{historyMessage("recent", "turn-17", "recent")}}, wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("recent"), Page: pointer(1)}, wire.SessionFeedDelivery{Kind: "sync", Source: "exact", NextCursor: pointer("live-cursor"), HistoryBefore: pointer("page-2")})
		case "page-2", "page-1":
			older.Add(1)
			if r.URL.Query().Get("history_turns") != "8" || r.URL.Query().Has("after") || r.Header.Get("Last-Event-ID") != "" {
				t.Errorf("older page changed live cursor or window: %s", r.URL)
			}
			first, last, next := 9, 16, "page-1"
			if before == "page-1" {
				first, last, next = 1, 8, ""
			}
			events := make([]wire.Envelope, 0, 12)
			for turn := first; turn <= last; turn++ {
				id := fmt.Sprintf("turn-%d", turn)
				events = append(events, historyMessage("message-"+id, id, id))
			}
			if before == "page-2" {
				decided := historyReview("approved-12", "turn-12", "approval-12", "shared-provider-call", "approved")
				events = append(events, decided, historyReview("approved-12-repeat", "turn-12", "approval-12", "shared-provider-call", "approved"), historyReview("late-progress-12", "turn-12", "approval-12", "shared-provider-call", "in_progress"))
			} else {
				events = append(events, historyReview("denied-2", "turn-2", "approval-2", "shared-provider-call", "denied"), historyReview("failed-3", "turn-3", "approval-3", "failed-call", "failed"))
			}
			historyFrames(w, wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: events}, wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer(next)})
		default:
			t.Errorf("unexpected history boundary %q", before)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	s.state.LastReceipt = api.Receipt{ID: "original-receipt", Outcome: "accepted"}
	s.state.ProjectionVersion = currentProjectionVersion - 1
	s.state.Views["main"].Cursor = "old-projection-cursor"
	s.state.Views["main"].Items = []api.Item{{ID: "stale", Text: "stale projection"}}
	s.state.Views["main"].Reviews = map[string]reviewFact{"stale": {Review: api.Review{ID: "stale", Status: "approved"}}}
	if err := privateWrite(s.path, s.state); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil || restored.Views["main"].Cursor != "" || len(restored.Views["main"].Items) != 0 || len(restored.Views["main"].Reviews) != 0 {
		t.Fatal("projection migration did not discard derived display", err)
	}
	s.state = restored
	if err := s.watch(t.Context(), s.client, "main", "instance"); err != io.EOF {
		t.Fatal("recent replacement did not complete", err)
	}
	v := s.state.Views["main"]
	if recent.Load() != 1 || older.Load() != 0 || len(v.Items) != 1 || len(s.Snapshot().Reviews) != 0 || !s.Snapshot().CanSend {
		t.Fatal("startup loaded older reviews or made extra history requests", recent.Load(), older.Load(), s.Snapshot())
	}
	v.Cursor = "invalid-cursor"
	if err := s.watch(t.Context(), s.client, "main", "instance"); err != io.EOF {
		t.Fatal("cursor replacement did not complete", err)
	}
	v = s.state.Views["main"]
	if recent.Load() != 2 || older.Load() != 0 || len(v.Items) != 1 || len(v.Reviews) != 0 {
		t.Fatal("cursor replacement traversed older Turns", recent.Load(), older.Load(), v.Items, v.Reviews)
	}
	cursor, seen, receipt := v.Cursor, clone(v.Seen), s.state.LastReceipt
	load := func() {
		t.Helper()
		if err := s.LoadEarlier(t.Context()); err != nil {
			t.Fatal(err)
		}
		if v.Cursor != cursor || !reflect.DeepEqual(v.Seen, seen) || !reflect.DeepEqual(s.state.LastReceipt, receipt) || !s.Snapshot().CanSend {
			t.Fatal("older page changed live cursor, seen watermark, receipt or admission")
		}
	}
	load()
	approvedID := reviewID("main", "turn-12", "approval-12")
	deniedID := reviewID("main", "turn-2", "approval-2")
	if len(v.Items) != 9 || len(v.Reviews) != 1 || v.Reviews[approvedID].Status != "approved" || v.Reviews[deniedID].Status != "" || older.Load() != 1 {
		t.Fatal("first explicit page exposed facts outside its eight Turns", len(v.Items), v.Reviews, older.Load())
	}
	// A repeated page can follow a retry or overlap without duplicating either
	// messages or decisions. It still does not alter the live stream watermark.
	v.HistoryBefore = "page-2"
	load()
	if len(v.Items) != 9 || len(v.Reviews) != 1 || older.Load() != 2 {
		t.Fatal("repeated page duplicated display facts", len(v.Items), v.Reviews, older.Load())
	}
	load()
	if len(v.Items) != 17 || len(v.Reviews) != 2 || v.Reviews[deniedID].Status != "denied" || v.Reviews[approvedID].ToolCallID != v.Reviews[deniedID].ToolCallID || approvedID == deniedID || s.Snapshot().HasEarlier {
		t.Fatal("second page lost distinct original review identities", len(v.Items), v.Reviews)
	}
}

func TestHistoryFailureAndCancellationDoNotBlockSubmit(t *testing.T) {
	for _, scenario := range []string{"invalid-page", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			var posts atomic.Int32
			started := make(chan struct{}, 1)
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					if !strings.HasSuffix(r.URL.Path, "/application/sessions/main/prompt") {
						t.Errorf("unexpected mutation route: %s", r.URL)
					}
					var input wire.ApplicationPromptRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil || value(input.OperationId) != "new-input" {
						t.Errorf("new Submit lost its own receipt: %v", err)
					}
					writeFixture(w, wire.CommandResult{OperationId: value(input.OperationId), Outcome: "accepted"})
					return
				}
				if r.Method != http.MethodGet || r.URL.Query().Get("history_turns") != "8" || r.URL.Query().Get("history_before") != "before" {
					t.Errorf("unexpected history route: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if scenario == "cancelled" {
					started <- struct{}{}
					<-r.Context().Done()
					return
				}
				historyFrames(w, wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("page")}, wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("page"), Events: []wire.Envelope{historyMessage("older", "old", "older")}})
			})
			v := s.state.Views["main"]
			v.HistoryBefore, v.Cursor = "before", "live"
			v.Items = []api.Item{{ID: "existing", Text: "keep"}}
			v.Reviews = map[string]reviewFact{"existing-review": {Review: api.Review{ID: "existing-review", Status: "denied"}}}
			s.state.LastReceipt = api.Receipt{ID: "existing-receipt", Outcome: "accepted"}
			before, receipt := clone(v), s.state.LastReceipt
			if scenario == "cancelled" {
				ctx, cancel := context.WithCancel(t.Context())
				result := make(chan error, 1)
				go func() { result <- s.LoadEarlier(ctx) }()
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("history request never started")
				}
				if !reflect.DeepEqual(before, v) || !s.connected {
					t.Fatal("pending history changed live state")
				}
				if receipt, err := s.Submit(t.Context(), api.Submission{ID: "new-input", Text: "new message"}, nil); err != nil || receipt.Outcome != "accepted" {
					t.Fatal("pending history blocked Submit", receipt, err)
				}
				cancel()
				if err := <-result; err == nil {
					t.Fatal("cancelled page reported success")
				}
			} else {
				if err := s.LoadEarlier(t.Context()); err == nil {
					t.Fatal("truncated page reported success")
				}
				if !reflect.DeepEqual(before, v) || !reflect.DeepEqual(receipt, s.state.LastReceipt) || !s.connected {
					t.Fatal("failed page changed current state")
				}
				if receipt, err := s.Submit(t.Context(), api.Submission{ID: "new-input", Text: "new message"}, nil); err != nil || receipt.Outcome != "accepted" {
					t.Fatal("failed page blocked Submit", receipt, err)
				}
			}
			if !reflect.DeepEqual(before, v) || posts.Load() != 1 || !s.connected {
				t.Fatal("history failure changed display or replayed a mutation", v, posts.Load())
			}
		})
	}
}

func TestHistoryPageByteBudgetPreservesLiveState(t *testing.T) {
	payload := strings.Repeat("x", 6<<20)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("history_turns") != "8" {
			t.Errorf("unexpected history window: %s", r.URL)
		}
		deliveries := make([]wire.SessionFeedDelivery, 3)
		for i := range deliveries {
			deliveries[i] = wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{historyMessage(fmt.Sprintf("large-%d", i), fmt.Sprintf("turn-%d", i), payload)}}
		}
		historyFrames(w, append(deliveries, wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})...)
	})
	v := s.state.Views["main"]
	v.HistoryBefore, v.Cursor = "before", "live"
	v.Items = []api.Item{{ID: "existing", Text: "keep"}}
	before := clone(v)
	if err := s.LoadEarlier(t.Context()); err == nil || !reflect.DeepEqual(before, v) || !s.connected {
		t.Fatal("oversized page changed the live view or connection", err)
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
