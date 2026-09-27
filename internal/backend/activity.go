package backend

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Select before trimming pet/chat payloads so a later completed parallel tool
// cannot hide an earlier active one. Never revive work from history or a lost
// connection. Approval/review/interrupt presentation takes precedence in the UI.
func currentActivity(v api.Snapshot) *api.Activity {
	if v.Connection != "ready" || v.Phase != "working" || v.CurrentTurn == "" {
		return nil
	}
	for i := len(v.Items) - 1; i >= 0; i-- {
		item := v.Items[i]
		if item.TurnKey == v.CurrentTurn && item.Kind == "activity" && item.Activity != nil && item.Status == "inProgress" {
			activity := *item.Activity
			activity.Target = strings.Join(strings.Fields(activity.Target), " ")
			if target := []rune(activity.Target); len(target) > 96 {
				activity.Target = string(target[:96]) + "…"
			}
			return &activity
		}
	}
	return nil
}
