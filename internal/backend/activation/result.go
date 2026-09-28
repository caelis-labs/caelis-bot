package activation

import (
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Observe uses the exact same quiet projection as chat. Native turn identity and
// lifecycle come from the adapter. Human steering ends automatic attribution;
// visibility already observed (including an earlier approval) is monotonic.
func Observe(previous api.BackgroundResult, id, turn, status string, items []api.Item, approval bool, now time.Time) api.BackgroundResult {
	out := previous
	out.ID = id
	if turn == "" || out.Complete {
		return out
	}
	for _, item := range items {
		if item.TurnKey == turn && item.Kind == "user" {
			out.Complete = true
			return out
		}
	}
	terminal := status == "completed" || status == "failed" || status == "interrupted" || status == "cancelled"
	visible := approval || status == "failed" || status == "interrupted"
	if terminal {
		v := Present(api.Snapshot{Items: items}, map[string]string{turn: status}, false)
		for _, item := range v.Items {
			if item.TurnKey == turn && (len(item.Artifacts) > 0 || item.Kind == "assistant" && strings.TrimSpace(item.Text) != "") {
				visible = true
			}
		}
	}
	out.Complete = terminal
	if visible && !out.Visible {
		out.Visible = true
		out.ObservedAt = now.UTC()
	}
	return out
}
