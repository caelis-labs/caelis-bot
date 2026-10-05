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

func TestHostReportSubmissionIsHiddenThroughReplayAndReload(t *testing.T) {
	const notice = "Task task-42 is completed."
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		var req wire.ApplicationPromptRequest
		if !strings.HasSuffix(r.URL.Path, "/application/sessions/main/prompt") || json.NewDecoder(r.Body).Decode(&req) != nil || req.SourceKind != "application_summary" || value(req.Input) != notice {
			t.Errorf("wrong host report submission: %s %+v", r.URL.Path, req)
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("host-turn")}})
	})
	in := api.Submission{ID: "host-report-42", Text: notice}
	if receipt, err := s.SubmitReport(t.Context(), in); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Task task-42 is completed."}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"The requested work is ready."}}`)
	events := []wire.Envelope{
		{EventId: pointer("host-input"), TurnId: pointer("host-turn"), Update: &user},
		{EventId: pointer("host-reply"), TurnId: pointer("host-turn"), Update: &reply},
		{EventId: pointer("host-end"), Kind: "caelis/lifecycle", TurnId: pointer("host-turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
	}
	v := s.state.Views["main"]
	for _, event := range events {
		s.applyScheduledEnvelope(v, event)
	}
	got := s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "The requested work is ready." {
		t.Fatal("host input leaked or reply hidden", got.Items)
	}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" {
		t.Fatal("reload leaked host input", got.Items)
	}
	replay := &view{State: v.State, Seen: map[string]bool{}}
	for _, event := range events {
		s.applyScheduledEnvelope(replay, event)
	}
	s.state.Views["main"] = replay
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" {
		t.Fatal("canonical replay leaked host input", got.Items)
	}
	s.applyScheduledEnvelope(replay, wire.Envelope{EventId: pointer("human-input"), TurnId: pointer("host-turn"), InputOperationId: pointer("human-operation"), Update: &user})
	got = s.Snapshot()
	if len(got.Items) != 2 || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal("same-text user input hidden", got.Items)
	}
}

func TestHostReportIsHiddenInOlderCaelisPage(t *testing.T) {
	const notice = "Task task-42 is completed."
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Task task-42 is completed."}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Reported result"}}`)
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("history_before") != "older" {
			t.Errorf("wrong history request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, value any) {
			b, _ := json.Marshal(value)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("caelis.control.bootstrap", wire.SessionState{SessionId: "main", ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1"})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "append_page", Source: "exact", Events: []wire.Envelope{
			{EventId: pointer("old-host"), TurnId: pointer("host-turn"), Update: &user},
			{EventId: pointer("old-answer"), TurnId: pointer("host-turn"), Update: &reply},
			{EventId: pointer("old-human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user},
		}})
		send("caelis.control.delivery", wire.SessionFeedDelivery{Kind: "sync", Source: "exact", HistoryBefore: pointer("")})
	})
	s.state.Operations["host-report-42"] = journal{Path: "/application/sessions/main/prompt", Outcome: "accepted", TurnID: "host-turn", Source: wire.ApplicationSource{Kind: "application_summary"}}
	s.state.Views["main"].HistoryBefore = "older"
	if err := s.LoadEarlier(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := s.Snapshot()
	if len(got.Items) != 2 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "Reported result" || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal("older page leaked host report or hid ordinary content", got.Items)
	}
}
