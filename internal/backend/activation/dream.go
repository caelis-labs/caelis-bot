package activation

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
)

// Dream presents only the last completed assistant recap. It never hides an
// approval or connection failure, and never interprets ordinary user prose.
func Dream(v api.Snapshot, turns map[string]string, pending bool) api.Snapshot {
	v.Maintenance = ""
	last := map[string]string{}
	for _, item := range v.Items {
		if item.Kind == "assistant" && strings.TrimSpace(item.Text) != "" {
			last[item.TurnKey] = item.ID
		}
	}
	current := v.CurrentTurn
	if current == "" && len(v.Items) > 0 {
		current = v.Items[len(v.Items)-1].TurnKey
	}
	out := make([]api.Item, 0, len(v.Items))
	for _, item := range v.Items {
		if status, dream := turns[item.TurnKey]; dream {
			if status != "completed" || item.Kind != "assistant" || item.ID != last[item.TurnKey] {
				continue
			}
			item.Artifacts = []api.Artifact{}
		}
		out = append(out, item)
	}
	v.Items = out
	if _, dream := turns[current]; dream || pending {
		v.Scheduled = true
		v.Quiet = v.Connection == "ready" && v.Message == "" && v.Phase != "unknown" && v.Phase != "failed" && v.Phase != "interrupting"
		for _, a := range v.Approvals {
			if a.Status != "resolved" {
				v.Quiet = false
			}
		}
		if hasReviewNotice(v.Reviews) {
			v.Quiet = false
		}
		if v.Quiet {
			v.Reviews = []api.Review{}
			status := turns[current]
			if v.Phase == "working" && v.CanInterrupt && (status == "running" || status == "inProgress" || status == "started") {
				v.Maintenance = "dreaming"
			}
		}
	}
	return v
}
