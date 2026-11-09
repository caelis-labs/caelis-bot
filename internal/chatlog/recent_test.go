package chatlog

import (
	"reflect"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRecentSnapshotMatchesFullPreviewAndDoesNotExposePendingInputs(t *testing.T) {
	l := &Log{pendingInputs: map[string]api.Item{"input:pending": {}}, items: []api.Item{
		{ID: "old", Kind: "user", TurnKey: "old"},
		{ID: "old-answer", Kind: "assistant", TurnKey: "old", Text: "old"},
		{ID: "input", Kind: "user", TurnKey: "current"},
		{ID: "first", Kind: "assistant", TurnKey: "current"},
		{ID: "tool", Kind: "tool", TurnKey: "current"},
		{ID: "last", Kind: "assistant", TurnKey: "current", Quoted: &api.QuotedMessage{Text: "quote"}},
		{ID: "pending", Kind: "user", TurnKey: "current", RequestID: "pending"},
	}}
	for _, turn := range []string{"", "current", "old", "missing"} {
		full, _ := l.Snapshot()
		expected := make([]api.Item, 0, 2)
		for _, item := range full {
			if turn != "" && item.TurnKey != turn {
				continue
			}
			if item.Kind == "user" {
				expected = expected[:0]
			}
			if item.Kind != "user" && item.Kind != "assistant" {
				continue
			}
			for i, prior := range expected {
				if prior.Kind == item.Kind {
					expected = append(expected[:i], expected[i+1:]...)
					break
				}
			}
			expected = append(expected, item)
		}
		recent, _ := l.RecentSnapshot(turn)
		if !reflect.DeepEqual(expected, recent) {
			t.Fatal("bounded IM changed full preview selection", turn, expected, recent)
		}
		for i := range recent {
			if recent[i].Quoted != nil {
				recent[i].Quoted.Text = "mutated"
			}
		}
		if l.items[5].Quoted.Text != "quote" {
			t.Fatal("bounded preview mutated IM")
		}
	}
}
