package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"testing"
)

func TestScheduledCanonicalReplayUsesInputOperationIdentity(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) {})
	s.state.Operations["automatic"] = journal{Scheduled: true, Outcome: "accepted"}
	v := s.state.Views["main"]
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"private instruction"}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"[[CAELIS_REMINDER_SKIP]]"}}`)
	events := []wire.Envelope{
		{EventId: pointer("u"), TurnId: pointer("turn"), InputOperationId: pointer("automatic"), Update: &user},
		{EventId: pointer("a"), TurnId: pointer("turn"), Update: &reply},
		{EventId: pointer("end"), Kind: "caelis/lifecycle", TurnId: pointer("turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
	}
	for _, e := range events {
		s.applyScheduledEnvelope(v, e, inputScope{})
	}
	got := s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal(got)
	}
	if e := s.saveLocked(); e != nil {
		t.Fatal(e)
	}
	restored, e := loadBinding(s.path)
	if e != nil {
		t.Fatal(e)
	}
	s.state = restored
	v = s.state.Views["main"]
	got = s.Snapshot()
	if !got.Quiet || len(got.Items) != 0 {
		t.Fatal("durable reload leaked", got)
	}
	// Replacement replay must classify the same native operation, without dispatch.
	replacement := &view{State: v.State, Seen: map[string]bool{}}
	for _, e := range events {
		s.applyScheduledEnvelope(replacement, e, inputScope{})
	}
	s.state.Views["main"] = replacement
	got = s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal("replacement leaked", got)
	}
	s.applyScheduledEnvelope(replacement, wire.Envelope{EventId: pointer("human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user}, inputScope{})
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "user" {
		t.Fatal("human hidden", got)
	}
}

func TestScheduledOriginPersistsBeforeNativeDispatch(t *testing.T) {
	var session *Session
	session = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		persisted, err := loadBinding(session.path)
		if err != nil {
			t.Fatal(err)
		}
		j := persisted.Operations["scheduled-input"]
		if len(j.OriginRequests) != 1 || j.OriginRequests[0] != "original-phone-input" || j.OriginOwner == "" {
			t.Fatal("source was not durable before dispatch")
		}
		writeFixture(w, wire.CommandResult{OperationId: "scheduled-input", Outcome: "accepted", Resource: &wire.CommandResource{Ref: pointer("turn")}})
	})
	in := api.Submission{ID: "scheduled-input", Scheduled: true, OriginRequests: []string{"original-phone-input"}}
	_, err := session.command(t.Context(), in.ID, "/sessions/main/prompt", wire.PromptRequest{OperationId: pointer(in.ID), Input: pointer("fixture"), SourceKind: pointer("authorized_background")}, in)
	if err != nil {
		t.Fatal(err)
	}
	j := session.state.Operations[in.ID]
	j.TurnID = "scheduled-turn"
	session.state.Operations[in.ID] = j
	session.state.Views["main"].Turns = map[string]string{"scheduled-turn": "completed"}
	if err := session.saveLocked(); err != nil {
		t.Fatal(err)
	}
	session.state, err = loadBinding(session.path)
	if err != nil {
		t.Fatal(err)
	}
	got := session.Snapshot()
	if len(got.ScheduledSources) != 1 || got.ScheduledSources[0].RequestID != "original-phone-input" || got.ScheduledSources[0].RuntimeOwner != got.RuntimeOwner || got.ScheduledSources[0].Phase != "completed" {
		t.Fatal(got.ScheduledSources)
	}
	raw, _ := json.Marshal(got)
	var visible map[string]any
	_ = json.Unmarshal(raw, &visible)
	if _, ok := visible["ScheduledSources"]; ok {
		t.Fatal("native provenance escaped")
	}
}
