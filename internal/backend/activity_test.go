package backend

import (
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestActivitySurvivesPreviewFilteringAndParallelCompletion(t *testing.T) {
	e := &snapshotEngine{value: api.Snapshot{Connection: "ready", Phase: "working", CurrentTurn: "current", Items: []api.Item{
		{ID: "old", TurnKey: "old", Kind: "activity", Status: "inProgress", Activity: &api.Activity{Kind: "edit"}},
		{ID: "read", TurnKey: "current", Kind: "activity", Status: "inProgress", Activity: &api.Activity{Kind: "read", Target: "README.md"}},
		{ID: "steer", TurnKey: "current", Kind: "user", Text: "new instruction"},
		{ID: "search", TurnKey: "current", Kind: "activity", Status: "completed", Activity: &api.Activity{Kind: "web"}},
	}}}
	s := NewService(e, nil, nil, nil, nil)
	for _, v := range []api.Snapshot{s.PetSnapshot(), s.ChatSnapshot(0, "").Snapshot} {
		if v.Activity == nil || v.Activity.Kind != "read" {
			t.Fatalf("lost active parallel tool: %+v", v.Activity)
		}
	}
	e.value.Items[1].Status = "completed"
	if s.PetSnapshot().Activity != nil {
		t.Fatal("completed or old work still shown")
	}
	e.value.Items[1].Status = "inProgress"
	for _, phase := range []string{"completed", "interrupted", "failed", "unknown", "interrupting", "waiting_approval"} {
		e.value.Phase = phase
		if s.PetSnapshot().Activity != nil {
			t.Fatal("stale work for phase", phase)
		}
	}
	e.value.Phase, e.value.Connection = "working", "offline"
	if s.PetSnapshot().Activity != nil {
		t.Fatal("disconnected tool shown as running")
	}
}

func TestPetMarkdownIsCompleteAndDoesNotMutateHistory(t *testing.T) {
	text := "# Result\n\n" + strings.Repeat("正文 **bold** `code`\n\n", 200) + "[End](https://example.com)"
	e := snapshotEngine{value: api.Snapshot{Items: []api.Item{{Kind: "assistant", Text: text, Details: "private tool output"}}}}
	s := NewService(e, nil, nil, nil, nil)
	if got := s.PetSnapshot().Items; len(got) != 1 || got[0].Text != text || got[0].Details != "" {
		t.Fatal("hover cannot read the complete Markdown")
	}
	if s.Snapshot().Items[0].Details != "private tool output" {
		t.Fatal("mutated native history")
	}
}
