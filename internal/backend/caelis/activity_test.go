package caelis

import (
	"encoding/json"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestToolActivitySparseUpdatesAndObservations(t *testing.T) {
	v := &view{Seen: map[string]bool{}}
	apply := func(body string, parent *wire.ParentToolRelation, turn string) {
		update := json.RawMessage(body)
		applyEnvelope(v, wire.Envelope{TurnId: pointer(turn), Update: &update, ParentTool: parent})
	}
	apply(`{"sessionUpdate":"tool_call","toolCallId":"read","kind":"read","name":"ReadFile","locations":[{"path":"/work/notes.md"}]}`, nil, "turn")
	apply(`{"sessionUpdate":"tool_call_update","toolCallId":"read","status":"in_progress"}`, nil, "turn")
	apply(`{"sessionUpdate":"tool_call_update","toolCallId":"read","name":"ReadFile"}`, nil, "turn")
	if len(v.Items) != 1 || v.Items[0].Activity.Kind != "read" || v.Items[0].Activity.Target != "notes.md" || v.Items[0].Status != "inProgress" {
		t.Fatalf("sparse update lost metadata: %+v", v.Items)
	}
	parent := &wire.ParentToolRelation{ToolCallId: pointer("read")}
	apply(`{"sessionUpdate":"tool_call","toolCallId":"child","kind":"execute"}`, parent, "child-turn")
	if len(v.Items) != 1 || value(v.State.Run.TurnId) != "turn" {
		t.Fatal("child observation took foreground")
	}
	apply(`{"sessionUpdate":"tool_call_update","toolCallId":"read","status":"completed"}`, parent, "task-turn")
	apply(`{"sessionUpdate":"tool_call","toolCallId":"read","kind":"execute"}`, nil, "turn")
	if len(v.Items) != 1 || v.Items[0].Status != "completed" || v.Items[0].Activity.Kind != "read" {
		t.Fatal("late start revived completed tool")
	}
	apply(`{"sessionUpdate":"tool_call","toolCallId":"search","kind":"search","name":"WebSearch"}`, nil, "turn")
	if v.Items[1].Activity.Kind != "web" {
		t.Fatal("web search not distinguished")
	}
}
