package caelis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestReviewReplacementHistoryCannotRetireCurrentBootstrapTurn(t *testing.T) {
	bootstrap := wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{TurnId: pointer("B"), Status: pointer("unknown"), Active: pointer(false)}}
	aRunning := wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "running"}}
	aDone := wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "completed"}}
	bRunning := wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "running"}}
	bFailed := wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "failed", Reason: pointer("current B failed")}}
	s := fixtureSession(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(bootstrap)
		fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
		for _, d := range []wire.SessionFeedDelivery{
			// Core may fall back to a full canonical replacement after an exact
			// subscription has synced and its spool becomes unavailable.
			{Kind: "sync", Source: "exact"},
			{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("history")},
			{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("history"), Events: []wire.Envelope{aRunning, aDone, bRunning}},
			{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("history"), Page: pointer(1)},
			{Kind: "sync", Source: "exact"},
			{Kind: "append_page", Source: "exact", Events: []wire.Envelope{bFailed}},
		} {
			b, _ := json.Marshal(d)
			fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
		}
	})
	s.state.Views["main"].State.Run = wire.RunState{TurnId: pointer("A"), Status: pointer("unknown"), Active: pointer(false)}
	if err := s.watch(t.Context(), s.client, "main", "instance"); err != io.EOF {
		t.Fatal(err)
	}
	v := s.state.Views["main"]
	if value(v.State.Run.TurnId) != "B" || value(v.State.Run.Status) != "failed" || v.Failure != "current B failed" {
		t.Fatalf("current B terminal lost after replacement: turn=%q status=%q failure=%q retired=%v", value(v.State.Run.TurnId), value(v.State.Run.Status), v.Failure, v.RetiredTurns)
	}
}

func TestCanonicalReplacementKeepsBootstrapGeneration(t *testing.T) {
	for _, owner := range []string{"main", "worker"} {
		for _, result := range []string{"completed", "failed", "unknown_active"} {
			for _, emptyHistory := range []bool{false, true} {
				name := fmt.Sprintf("%s/%s/empty=%t", owner, result, emptyHistory)
				t.Run(name, func(t *testing.T) {
					sid := "main"
					if owner == "worker" {
						sid = "work"
					}
					bootstrap := wire.SessionState{SessionId: sid, ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{TurnId: pointer("B"), RunId: pointer("run-B"), HandleId: pointer("handle-B"), Status: pointer("unknown"), Active: pointer(result == "unknown_active")}}
					lifecycle := func(turn, state string) wire.Envelope {
						return wire.Envelope{Kind: "caelis/lifecycle", SessionId: pointer(sid), TurnId: pointer(turn), Lifecycle: &wire.LifecycleEvent{State: state}}
					}
					var replay []wire.Envelope
					if !emptyHistory {
						replay = []wire.Envelope{lifecycle("A", "running"), lifecycle("A", "completed"), lifecycle("B", "running")}
					}
					deliveries := []wire.SessionFeedDelivery{{Kind: "sync", Source: "exact"}, {Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("full")}}
					if !emptyHistory {
						deliveries = append(deliveries, wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("full"), Events: replay})
					}
					page := 0
					if !emptyHistory {
						page = 1
					}
					deliveries = append(deliveries, wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("full"), Page: pointer(page)}, wire.SessionFeedDelivery{Kind: "sync", Source: "exact"})
					if result != "unknown_active" {
						terminal := lifecycle("B", result)
						if result == "failed" {
							terminal.Lifecycle.Reason = pointer("current failure")
						}
						deliveries = append(deliveries, wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{terminal}})
					}
					// A's delayed terminal and error must remain attached to A.
					deliveries = append(deliveries, wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{lifecycle("A", "completed"), {Kind: "caelis/error", SessionId: pointer(sid), TurnId: pointer("A"), Error: pointer("old failure")}}})
					s := fixtureSession(t, func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						b, _ := json.Marshal(bootstrap)
						fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
						for _, d := range deliveries {
							b, _ := json.Marshal(d)
							fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
						}
					})
					s.state.LastReceipt = api.Receipt{ID: "original-request", Outcome: "unknown"}
					original := commandResultEvidence{TurnID: "A", Handle: "original-handle", TurnEnded: true, Received: true}
					v := s.state.Views["main"]
					v.State.Run = wire.RunState{TurnId: pointer("A"), Status: pointer("unknown"), Active: pointer(false)}
					if owner == "worker" {
						v.State.Run = wire.RunState{TurnId: pointer("resident"), Status: pointer("running"), Active: pointer(true)}
						s.state.Workers["task"] = worker{Binding: wire.ApplicationBinding{SessionId: sid}, Task: api.Task{ID: "task", Status: "unknown"}}
						v = &view{State: wire.SessionState{SessionId: sid, Run: wire.RunState{TurnId: pointer("A"), Status: pointer("unknown"), Active: pointer(false)}}, Seen: map[string]bool{}}
						s.state.Views[sid] = v
					}
					v.CommandResults = map[string]commandResultEvidence{"original": original}
					if err := s.watch(t.Context(), s.client, sid, "instance"); err != io.EOF {
						t.Fatal(err)
					}
					v = s.state.Views[sid]
					if value(v.State.Run.TurnId) != "B" || value(v.State.Run.RunId) != "run-B" || value(v.State.Run.HandleId) != "handle-B" || v.RetiredTurns["B"] || !v.RetiredTurns["A"] {
						t.Fatalf("replacement generation: run=%+v retired=%v", v.State.Run, v.RetiredTurns)
					}
					if got := v.CommandResults["original"]; got != original || s.state.LastReceipt.ID != "original-request" || s.state.LastReceipt.Outcome != "unknown" {
						t.Fatalf("original receipt changed: command=%+v receipt=%+v", got, s.state.LastReceipt)
					}
					wantStatus, wantActive := result, false
					if result == "unknown_active" {
						wantStatus, wantActive = "unknown", true
					}
					if value(v.State.Run.Status) != wantStatus || value(v.State.Run.Active) != wantActive || v.Failure == "old failure" {
						t.Fatalf("current outcome lost or old outcome leaked: run=%+v failure=%q", v.State.Run, v.Failure)
					}
					if result == "failed" && v.Failure != "current failure" {
						t.Fatalf("failure reason lost: %q", v.Failure)
					}
					if owner == "main" {
						snap := s.Snapshot()
						wantPhase := wantStatus
						if snap.CurrentTurn != "B" || snap.Phase != wantPhase {
							t.Fatalf("resident result hidden: turn=%q phase=%q", snap.CurrentTurn, snap.Phase)
						}
					} else {
						work := s.WorkStates()
						wantWork := wantStatus
						if wantActive {
							wantWork = "working"
						}
						if len(work) != 1 || work[0].ExecutionKey != "B" || work[0].Task.Status != wantWork || value(s.state.Views["main"].State.Run.TurnId) != "resident" || value(s.state.Views["main"].State.Run.Status) != "running" {
							t.Fatalf("worker result hidden or resident changed: work=%+v main=%+v", work, s.state.Views["main"].State.Run)
						}
					}
				})
			}
		}
	}
}

func TestReplacementHeadSurvivesCheckpointBeforeCurrentTerminal(t *testing.T) {
	bootstrap := wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{TurnId: pointer("B"), Status: pointer("unknown"), Active: pointer(false)}}
	s := fixtureSession(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(bootstrap)
		fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
		for _, d := range []wire.SessionFeedDelivery{
			{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("full")},
			{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("full"), Events: []wire.Envelope{
				{Kind: "caelis/lifecycle", TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "running"}},
				{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "running"}},
			}},
			{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("full"), Page: pointer(1)},
			{Kind: "sync", Source: "exact"},
		} {
			b, _ := json.Marshal(d)
			fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
		}
	})
	s.state.Views["main"].State.Run.TurnId = pointer("A")
	if err := s.watch(t.Context(), s.client, "main", "instance"); err != io.EOF {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	v := restored.Views["main"]
	if v == nil || v.RetiredTurns["B"] || !v.RetiredTurns["A"] || value(v.State.Run.TurnId) != "B" {
		t.Fatalf("checkpoint head changed: %+v", v)
	}
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("A"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("B"), Lifecycle: &wire.LifecycleEvent{State: "failed", Reason: pointer("current failed")}})
	if value(v.State.Run.TurnId) != "B" || value(v.State.Run.Status) != "failed" || v.Failure != "current failed" {
		t.Fatalf("restart lost current terminal: run=%+v failure=%q", v.State.Run, v.Failure)
	}
}
