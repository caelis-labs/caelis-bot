package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Exercise the callback/result and ordinary submit entry points. The native
// fixture deliberately leaves the original Turn running after accepting result.
func TestTerminalReceiptFencesCorrectionUntilOriginalTurnEnds(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		for _, restart := range []bool{false, true} {
			name := "rejected-result"
			if lostResponse {
				name = "lost-response"
			}
			if restart {
				name += "/restart"
			}
			t.Run(name, func(t *testing.T) {
				call := wire.SessionToolCall{Id: "original-dream", CallId: "provider-call", SessionId: "main", PrincipalId: "owner", TurnId: "original-turn", ItemId: "item", Name: "bot_dream", ToolsVersion: "v1", Source: wire.SessionSource{Kind: "user", OperationId: "original-prompt"}, State: "pending"}
				var effects, checks, attempts, posts, steers, stops atomic.Int32
				var original []byte
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					switch {
					case strings.HasSuffix(r.URL.Path, "/claim"):
						claimed := call
						claimed.State = "claimed"
						writeFixture(w, claimed)
					case strings.HasSuffix(r.URL.Path, "/result"):
						var result wire.SessionToolCallResult
						if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
							t.Error(err)
						}
						b, _ := json.Marshal(result)
						if !value(result.TurnComplete) {
							t.Error("original terminal receipt changed")
						}
						if attempts.Add(1) == 1 {
							original = b
							if lostResponse {
								conn, _, _ := w.(http.Hijacker).Hijack()
								_ = conn.Close()
							} else {
								http.Error(w, "before acceptance", http.StatusServiceUnavailable)
							}
							return
						}
						if string(b) != string(original) {
							t.Error("recovery changed immutable receipt")
						}
						writeFixture(w, struct{}{})
					case strings.HasSuffix(r.URL.Path, "/steer"):
						steers.Add(1)
						writeFixture(w, wire.CommandResult{OperationId: "correction", Outcome: "committed", InputStatus: pointer("accepted")})
					case strings.HasSuffix(r.URL.Path, "/prompt"):
						posts.Add(1)
						var in wire.PromptRequest
						_ = json.NewDecoder(r.Body).Decode(&in)
						if value(in.OperationId) != "correction" || value(in.Input) != "Keep the original review; revoke deployment." {
							t.Error("input identity/bytes changed", in)
						}
						writeFixture(w, wire.CommandResult{OperationId: "correction", Outcome: "committed"})
					case strings.HasSuffix(r.URL.Path, "/cancel"):
						stops.Add(1)
						var in wire.CancelRequest
						_ = json.NewDecoder(r.Body).Decode(&in)
						writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed"})
					default:
						t.Errorf("unexpected %s", r.URL.Path)
						http.NotFound(w, r)
					}
				})
				s.state.Views["main"].State.Run = wire.RunState{Active: pointer(true), Status: pointer("running"), TurnId: pointer(call.TurnId), HandleId: pointer("handle"), RunId: pointer("run")}
				s.state.Context.Injected = true
				s.state.Operations["original-prompt"] = journal{Path: "/sessions/main/prompt", Outcome: "accepted", Source: call.Source}
				host := &acceptanceTools{call: func(context.Context, string, json.RawMessage) api.ToolResult {
					effects.Add(1)
					return api.ToolResult{TurnComplete: true, Content: []map[string]string{{"type": "text", "text": "original handoff"}}, CheckTurnComplete: func() error { checks.Add(1); return nil }}
				}}
				s.catalogs = map[string]map[string]api.ApplicationTools{"v1": {"bot_dream": host}}
				if err := s.handleCall(t.Context(), s.client, s.state.Session, s.state.PrincipalID, call); err == nil {
					t.Fatal("missing injected result failure")
				}
				if restart {
					restored := New(Options{Directory: filepath.Dir(s.path)})
					restored.client, restored.connected = s.client, true
					s = restored
				}
				input := api.Submission{ID: "correction", Text: "Keep the original review; revoke deployment."}
				assertWaiting := func() {
					t.Helper()
					out, err := s.Submit(t.Context(), input, nil)
					if err != nil || out.Outcome != "rejected" || !out.Deferred || out.ID != input.ID || s.Snapshot().CanSteer {
						t.Fatalf("correction admitted before terminal confirmation: %+v %v steer=%v", out, err, s.Snapshot().CanSteer)
					}
				}
				assertWaiting()
				if err := s.handleCall(t.Context(), s.client, s.state.Session, s.state.PrincipalID, call); err != nil {
					t.Fatal(err)
				}
				assertWaiting() // result acceptance alone is not Turn completion.
				if err := s.Interrupt(t.Context()); err != nil {
					t.Fatal("receipt fence blocked native Stop", err)
				}
				if stops.Load() != 1 {
					t.Fatal("Stop did not use native cancellation")
				}
				s.mu.Lock()
				v := s.state.Views["main"]
				applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer(call.TurnId), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
				s.mu.Unlock()
				for range 2 {
					receipt, err := s.Submit(t.Context(), input, nil)
					if err != nil || receipt.Outcome != "accepted" {
						t.Fatal(receipt, err)
					}
				}
				if effects.Load() != 1 || checks.Load() != 1 || attempts.Load() != 2 || posts.Load() != 1 || steers.Load() != 0 {
					t.Fatal("receipt or input replay", effects.Load(), checks.Load(), attempts.Load(), posts.Load(), steers.Load())
				}
			})
		}
	}
}

func TestTerminalReceiptKeepsObservedFinalityAcrossHistoryReplacement(t *testing.T) {
	for _, evidence := range []string{"turn", "run", "unconfirmed"} {
		t.Run(evidence, func(t *testing.T) {
			var posts atomic.Int32
			bootstrap := wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", Run: wire.RunState{TurnId: pointer("later-turn"), Status: pointer("completed"), Active: pointer(false)}}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/prompt") {
					posts.Add(1)
					writeFixture(w, wire.CommandResult{OperationId: "correction", Outcome: "committed"})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				b, _ := json.Marshal(bootstrap)
				fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
				for _, d := range []wire.SessionFeedDelivery{
					{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("bounded")},
					{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("bounded")},
					{Kind: "sync", Source: "exact"},
				} {
					b, _ := json.Marshal(d)
					fmt.Fprintf(w, "event: caelis.control.delivery\ndata: %s\n\n", b)
				}
			})
			call := wire.SessionToolCall{Id: "original", SessionId: "main", TurnId: "original-turn"}
			receipt := &wire.SessionToolCallResult{Outcome: "succeeded", TurnComplete: pointer(true), Content: json.RawMessage(`[]`)}
			s.state.Calls[call.Id] = callRecord{Call: call, Phase: "completed", Receipt: receipt}
			s.state.Context.Injected = true
			v := s.state.Views["main"]
			v.State.Run = wire.RunState{TurnId: pointer("original-turn"), Status: pointer("running"), Active: pointer(true)}
			switch evidence {
			case "turn":
				v.Turns = map[string]string{"original-turn": "completed"}
			case "run":
				v.State.Run.Status = pointer("completed")
				v.State.Run.Active = pointer(false)
			}
			if err := s.watch(t.Context(), s.client, "main", "instance"); err != io.EOF {
				t.Fatal(err)
			}
			restored := New(Options{Directory: filepath.Dir(s.path)})
			restored.client, restored.connected = s.client, true
			input := api.Submission{ID: "correction", Text: "Continue the original review."}
			for range 2 {
				out, err := restored.Submit(t.Context(), input, nil)
				if evidence == "unconfirmed" {
					if err != nil || out.Outcome != "rejected" || !out.Deferred {
						t.Fatalf("later history invented finality: %+v %v", out, err)
					}
				} else if err != nil || out.Outcome != "accepted" {
					t.Fatalf("known finality lost during bounded history/restart: %+v %v", out, err)
				}
			}
			want := int32(1)
			if evidence == "unconfirmed" {
				want = 0
			}
			if posts.Load() != want {
				t.Fatalf("native POSTs=%d want=%d", posts.Load(), want)
			}
			got, _ := json.Marshal(restored.state.Calls[call.Id].Receipt)
			original, _ := json.Marshal(receipt)
			if string(got) != string(original) {
				t.Fatal("immutable original result changed")
			}
		})
	}
}
