package codex

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestNativeActivityUsesTypedActionsAndLifecycle(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tc := range []struct{ body, kind, target string }{
		{`{"id":"read","type":"commandExecution","status":"inProgress","command":"cat secret","commandActions":[{"type":"read","path":"/work/README.md"}]}`, "read", "README.md"},
		{`{"id":"search","type":"commandExecution","commandActions":[{"type":"search","path":null}]}`, "search", ""},
		{`{"id":"mixed","type":"commandExecution","commandActions":[{"type":"read"},{"type":"unknown"}]}`, "execute", ""},
		{`{"id":"shell","type":"commandExecution","command":"cat file; dangerous-command"}`, "execute", ""},
		{`{"id":"web","type":"webSearch"}`, "web", ""},
		{`{"id":"fetch","type":"webSearch","action":{"type":"openPage","url":"https://example.com"}}`, "fetch", ""},
		{`{"id":"patch","type":"fileChange","changes":[{"path":"/work/a.go"}]}`, "edit", "a.go"},
		{`{"id":"mcp","type":"mcpToolCall","tool":"lookup","arguments":{"token":"private"}}`, "tool", "lookup"},
	} {
		var item nativeItem
		if err := json.Unmarshal([]byte(tc.body), &item); err != nil {
			t.Fatal(err)
		}
		s.applyItem("turn", item, false)
		got := s.state.Items[s.items[opaque("turn", item.ID)]]
		if got.Activity == nil || got.Activity.Kind != tc.kind || got.Activity.Target != tc.target || got.Status != "inProgress" {
			t.Fatalf("bad activity: %+v", got)
		}
		completed := item
		completed.Status = "completed"
		s.applyItem("turn", completed, true)
		s.applyItem("turn", item, false)
		if got := s.state.Items[s.items[opaque("turn", item.ID)]].Status; got != "completed" {
			t.Fatal("late start revived a completed tool", got)
		}
	}
	if itemActivity(nativeItem{Type: "subAgentActivity"}) != nil {
		t.Fatal("child observation replaced foreground work")
	}
}

func TestRecentActivityRetainsToolBeforeSteeringInput(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	s.mu.Lock()
	s.run = "turn"
	s.state.CurrentTurn = opaque("turn")
	s.state.Items = []api.Item{
		{ID: "old", Kind: "user", TurnKey: opaque("old")},
		{ID: "tool", Kind: "activity", TurnKey: opaque("turn"), Status: "inProgress", Activity: &api.Activity{Kind: "read"}},
		{ID: "steer", Kind: "user", TurnKey: opaque("turn")},
	}
	s.publishSnapshot()
	s.mu.Unlock()
	got := s.RecentSnapshot()
	s.mu.Lock()
	s.run = ""
	s.mu.Unlock()
	if len(got.Items) != 2 || got.Items[0].ID != "tool" {
		t.Fatal("steering hid active tool owner", got.Items)
	}
}
