package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"testing"
)

func displayFrame(kind, text string, replacement bool) wire.Envelope {
	raw, _ := json.Marshal(map[string]any{"sessionUpdate": kind, "messageId": "same", "content": map[string]any{"type": "text", "text": text}, "replacement": replacement})
	update := json.RawMessage(raw)
	return wire.Envelope{TurnId: pointer("turn"), Update: &update}
}
func TestNegotiatedSameMessageReplacementIncludingEmpty(t *testing.T) {
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}, Items: []api.Item{}}
	transcript := newWorkerTranscript("worker")
	for _, frame := range []wire.Envelope{displayFrame("agent_message_chunk", "old", false), displayFrame("agent_message_chunk", "new", true), displayFrame("agent_message_chunk", "", true)} {
		applyEnvelope(v, frame)
		if err := transcript.apply(frame); err != nil {
			t.Fatal(err)
		}
	}
	if len(v.Items) != 1 || v.Items[0].Text != "" || len(transcript.items) != 1 || transcript.items[0].Text != "" {
		t.Fatal("replacement did not clear same message", v.Items, transcript.items)
	}
}
func TestNegotiatedToolDisplayClear(t *testing.T) {
	v := &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}, Items: []api.Item{}}
	for _, raw := range []string{
		`{"sessionUpdate":"tool_call","toolCallId":"tool","name":"ReadFile","kind":"read"}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"tool","replacedFields":["name","kind","locations"],"name":"","locations":[]}`,
	} {
		update := json.RawMessage(raw)
		applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &update})
	}
	if len(v.Items) != 1 || v.Items[0].Activity == nil || v.Items[0].Activity.Target != "" || v.Items[0].Activity.Kind != "tool" {
		t.Fatal("tool display replacement retained stale value", v.Items)
	}
}
