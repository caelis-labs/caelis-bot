package caelis

import (
	"encoding/json"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestAssistantStreamBoundaries(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	apply := func(body string) {
		update := json.RawMessage(body)
		applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &update})
	}
	chunk := `{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"text"}}`
	status := func(want string) {
		t.Helper()
		if v.Items[0].Status != want {
			t.Fatalf("reply status %q; want %q", v.Items[0].Status, want)
		}
	}
	apply(chunk)
	status("inProgress")
	apply(`{"sessionUpdate":"tool_call","toolCallId":"read","kind":"read"}`)
	status("completed")
	apply(chunk)
	status("inProgress")
	apply(`{"sessionUpdate":"tool_call_update","toolCallId":"read","status":"completed"}`)
	status("inProgress")
	for _, e := range []wire.Envelope{
		{Kind: "caelis/lifecycle", TurnId: pointer("worker"), Scope: pointer("participant"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
		{Kind: "caelis/lifecycle", TurnId: pointer("turn"), ApprovalRequestId: pointer("approval"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
	} {
		applyEnvelope(v, e)
		status("inProgress")
	}
	apply(`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"private reasoning"}}`)
	status("completed")
	apply(chunk)
	applyEnvelope(v, wire.Envelope{Kind: "caelis/lifecycle", TurnId: pointer("turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}})
	status("completed")
	if v.Items[0].Text != "texttexttext" || len(v.Items) != 2 {
		t.Fatal("boundaries changed text or exposed reasoning", v.Items)
	}
	update := json.RawMessage(chunk)
	applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &update, Final: pointer(true)})
	status("completed")
	apply(chunk)
	status("inProgress")
	empty := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":""}}`)
	applyEnvelope(v, wire.Envelope{TurnId: pointer("turn"), Update: &empty, Final: pointer(true)})
	status("completed")
}
