package caelis

import (
	"encoding/json"
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
		s.applyScheduledEnvelope(v, e)
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
		s.applyScheduledEnvelope(replacement, e)
	}
	s.state.Views["main"] = replacement
	got = s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal("replacement leaked", got)
	}
	s.applyScheduledEnvelope(replacement, wire.Envelope{EventId: pointer("human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user})
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "user" {
		t.Fatal("human hidden", got)
	}
}
