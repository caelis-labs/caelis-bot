package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
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
	s.state.Workers["task"] = worker{Task: api.Task{ID: "task", Status: "working"}, Binding: wire.ApplicationBinding{SessionId: "work"}}
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
	s.state.Workers["task"] = worker{Task: api.Task{ID: "task"}, Binding: wire.ApplicationBinding{SessionId: "work"}}
	if messages, err := s.ReadWorkConversation(t.Context(), "task"); err == nil || messages != nil {
		t.Fatal("unreadable native history became empty success", messages, err)
	}
}
