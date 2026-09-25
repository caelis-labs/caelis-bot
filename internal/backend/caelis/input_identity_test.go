package caelis

import (
	"encoding/json"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestUserInputIdentitySeparatesSteersInSameTurn(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	content := wire.ACPUpdate(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"same"}}`)
	for _, op := range []string{"first", "second"} {
		applyEnvelope(v, wire.Envelope{EventId: pointer(op), TurnId: pointer("turn"), InputOperationId: pointer(op), Update: &content})
	}
	if len(v.Items) != 2 || v.Items[0].RequestID != "first" || v.Items[1].RequestID != "second" || v.Items[0].ID == v.Items[1].ID {
		t.Fatalf("inputs merged or lost identity: %+v", v.Items)
	}
}

func TestPromptInputWaitsForExactReceiptAcrossReload(t *testing.T) {
	for _, receiptFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "event-first", true: "receipt-first"}[receiptFirst], func(t *testing.T) {
			s := &Session{state: binding{Session: wire.ApplicationBinding{SessionId: "resident"}, Operations: map[string]journal{
				"new": {Path: "/application/sessions/resident/prompt", Outcome: "unknown", PendingInput: &pendingInput{VisibleIDs: []string{"old"}}},
			}}}
			v := &view{Items: []api.Item{{ID: "old", Kind: "user", Text: "same", TurnKey: "old-turn"}}, Seen: map[string]bool{}}
			resolve := func() {
				j := s.state.Operations["new"]
				j.Outcome, j.TurnID, j.PendingInput = "accepted", "new-turn", nil
				s.state.Operations["new"] = j
			}
			if receiptFirst {
				resolve()
			}
			content := wire.ACPUpdate(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"same"}}`)
			applyEnvelope(v, wire.Envelope{EventId: pointer("event"), TurnId: pointer("new-turn"), Update: &content})
			// Receipt loss and app restart must preserve the pending visibility fence.
			raw, _ := json.Marshal(s.state)
			_ = json.Unmarshal(raw, &s.state)
			out := api.Snapshot{Items: clone(v.Items)}
			s.correlateUserInputs(&out)
			if !receiptFirst && (len(out.Items) != 1 || out.Items[0].ID != "old" || len(v.Items) != 2) {
				t.Fatalf("uncorrelated input leaked or canonical content lost: %+v", out.Items)
			}
			resolve()
			out.Items = clone(v.Items)
			s.correlateUserInputs(&out)
			if len(out.Items) != 2 || out.Items[0].RequestID != "" || out.Items[1].RequestID != "new" {
				t.Fatalf("receipt did not correlate exact turn: %+v", out.Items)
			}
		})
	}
}
