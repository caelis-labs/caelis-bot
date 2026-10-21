package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func workerHistoryFrames(w http.ResponseWriter, events []wire.Envelope, before string) {
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(name string, value any) {
		b, _ := json.Marshal(value)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
	}
	write("caelis.control.bootstrap", wire.SessionState{SessionId: "work", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
	write("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_begin", Source: "replacement", SnapshotId: pointer("history")})
	write("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_page", Source: "replacement", SnapshotId: pointer("history"), Events: events})
	write("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "replace_end", Source: "replacement", SnapshotId: pointer("history"), Page: pointer(1)})
	write("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer(before)})
}

func workerHistoryMessage(id, turn, kind, text string, final bool) wire.Envelope {
	b, _ := json.Marshal(map[string]any{"sessionUpdate": kind, "content": map[string]string{"type": "text", "text": text}})
	return wire.Envelope{Kind: "session/update", EventId: pointer(id), TurnId: pointer(turn), Final: pointer(final), Update: pointer(json.RawMessage(b))}
}

func TestWorkerConversationReadsNativePagesWithoutConsuming(t *testing.T) {
	requests := 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/sessions/work/reconnect") || r.URL.Query().Get("history_turns") != "64" {
			t.Errorf("worker read changed native state: %s %s", r.Method, r.URL)
		}
		requests++
		if r.URL.Query().Get("history_before") == "" {
			workerHistoryFrames(w, []wire.Envelope{
				workerHistoryMessage("u2", "t2", "user_message_chunk", "second", true),
				workerHistoryMessage("a2", "t2", "agent_message_chunk", "completed progress", true),
				workerHistoryMessage("a2b", "t2", "agent_message_chunk", "second progress", true),
				workerHistoryMessage("a3", "t3", "agent_message_chunk", "unfinished", false),
			}, "older")
		} else {
			workerHistoryFrames(w, []wire.Envelope{
				workerHistoryMessage("u1", "t1", "user_message_chunk", "first", true),
				workerHistoryMessage("a1", "t1", "agent_message_chunk", "first answer", true),
			}, "")
		}
	})
	s.state.Workers["task"] = worker{Task: api.Task{ID: "task", Status: "working"}, Binding: wire.SessionBinding{SessionId: "work"}}
	messages, err := s.ReadWorkConversation(t.Context(), "task")
	if err != nil || requests != 2 || len(messages) != 5 || messages[0].Role != "user" || messages[0].Text != "first" || messages[3].Text != "completed progress" || messages[4].Text != "second progress" || messages[3].ID == messages[4].ID {
		t.Fatal("native FIFO history projection", messages, requests, err)
	}
	if _, err := s.ReadWorkConversation(t.Context(), "other"); err == nil {
		t.Fatal("foreign worker history was readable")
	}
}

func TestWorkerConversationRejectsUnreadableNativeHistory(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	s.state.Workers["task"] = worker{Task: api.Task{ID: "task"}, Binding: wire.SessionBinding{SessionId: "work"}}
	if messages, err := s.ReadWorkConversation(t.Context(), "task"); err == nil || messages != nil {
		t.Fatal("unreadable native history became empty success", messages, err)
	}
}

// Core may omit the optional active flag on a terminal native run. Exercise
// the manager's state read before history, not only the transcript reader.
func TestTerminalWorkerConversationReadPath(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(fmt.Sprint(unreadable), func(t *testing.T) {
			states, histories := 0, 0
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("history read mutated the original owner: %s", r.Method)
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				switch r.URL.Path {
				case "/api/control/v1/sessions/work/state":
					states++
					writeFixture(w, wire.SessionState{SessionId: "work", Run: wire.RunState{Status: pointer("completed"), TurnId: pointer("t2")}})
				case "/api/control/v1/sessions/work/reconnect":
					histories++
					if unreadable {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					if r.URL.Query().Get("history_before") == "" {
						// Native chunks have messageId but no final flag; the
						// native lifecycle completes the final assistant message.
						message := workerHistoryMessage("", "t2", "agent_message_chunk", "final answer", false)
						var update map[string]any
						_ = json.Unmarshal(*message.Update, &update)
						update["messageId"] = "native-final"
						body, _ := json.Marshal(update)
						message.Update = pointer(json.RawMessage(body))
						workerHistoryFrames(w, []wire.Envelope{message, {Kind: "caelis/lifecycle", TurnId: pointer("t2"), Lifecycle: &wire.LifecycleEvent{State: "completed"}}}, "older")
					} else if r.URL.Query().Get("history_before") == "older" {
						workerHistoryFrames(w, []wire.Envelope{workerHistoryMessage("u1", "t1", "user_message_chunk", "original question", true)}, "")
					} else {
						t.Error("unexpected history boundary")
					}
				default:
					t.Errorf("read left original owner: %s", r.URL.Path)
				}
			})
			s.state.Workers["task"] = worker{Native: true, Task: api.Task{ID: "task", Status: "pending", Outcome: "accepted"}, Binding: wire.SessionBinding{SessionId: "work"}, PromptID: "original-prompt"}
			root := t.TempDir()
			m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			first, err := m.ReadTaskConversation(t.Context(), "task")
			if unreadable {
				if err == nil || first.Messages != nil || histories != 1 {
					t.Fatal("completed state hid a real history failure", err, histories)
				}
				return
			}
			if err != nil || first.Status != "completed" || len(first.Messages) != 2 || first.Messages[1].Text != "final answer" {
				t.Fatal("terminal original history was unreadable", err, len(first.Messages))
			}
			// Reopen the local coordinator and reconnect to the same native
			// session: neither task nor message identities may be replaced.
			m, err = tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			second, err := m.ReadTaskConversation(t.Context(), "task")
			if err != nil || !reflect.DeepEqual(first, second) || states != 2 || histories != 4 || s.state.Workers["task"].PromptID != "original-prompt" || len(s.state.Workers) != 1 {
				t.Fatal("read/reconnect changed original identities", err, states, histories)
			}
		})
	}
}

func TestWorkerConversationAcceptsExactPageCursorsAndRejectsMismatchedSSEID(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				b, _ := json.Marshal(wire.SessionState{SessionId: "work", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
				fmt.Fprintf(w, "event: caelis.control.bootstrap\ndata: %s\n\n", b)
				for i, event := range []wire.Envelope{workerHistoryMessage("u", "t", "user_message_chunk", "question", true), workerHistoryMessage("a", "t", "agent_message_chunk", "answer", true)} {
					cursor := fmt.Sprint(i + 1)
					id := cursor
					if mismatch {
						id = "wrong"
					}
					b, _ = json.Marshal(wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", NextCursor: &cursor, Events: []wire.Envelope{event}})
					fmt.Fprintf(w, "id: %s\nevent: caelis.control.delivery\ndata: %s\n\n", id, b)
				}
				b, _ = json.Marshal(wire.SessionFeedDelivery{Kind: "sync", Source: "exact", NextCursor: pointer("2")})
				fmt.Fprintf(w, "id: 2\nevent: caelis.control.delivery\ndata: %s\n\n", b)
			})
			s.state.Workers["task"] = worker{Task: api.Task{ID: "task"}, Binding: wire.SessionBinding{SessionId: "work"}}
			messages, err := s.ReadWorkConversation(t.Context(), "task")
			if mismatch {
				if err == nil || messages != nil {
					t.Fatal("mismatched cursor accepted")
				}
				return
			}
			if err != nil || len(messages) != 2 || messages[1].Text != "answer" {
				t.Fatal("exact native pages unreadable", err, len(messages))
			}
		})
	}
}
