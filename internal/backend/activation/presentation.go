// Package activation projects scheduled work without changing its native history
// or execution authority. Both adapters use the same quiet-result contract.
package activation

import (
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Present withholds scheduled prose until a terminal receipt. Only the exact
// skip response on an owned scheduled turn is silent; user text is never matched.
// A human steering that turn restores normal live presentation.
func Present(v api.Snapshot, scheduled map[string]string, pending bool) api.Snapshot {
	human := map[string]bool{}
	last := map[string]string{}
	artifacts := map[string]bool{}
	for _, i := range v.Items {
		if len(i.Artifacts) > 0 {
			artifacts[i.TurnKey] = true
		}
		if i.Kind == "user" {
			human[i.TurnKey] = true
		}
		if i.Kind == "assistant" && (strings.TrimSpace(i.Text) != "" || len(i.Artifacts) > 0) {
			last[i.TurnKey] = strings.TrimSpace(i.Text)
		}
	}
	current := v.CurrentTurn
	if current == "" && len(v.Items) > 0 {
		current = v.Items[len(v.Items)-1].TurnKey
	}
	_, active := scheduled[current]
	v.Scheduled = active && !human[current] || pending
	out := make([]api.Item, 0, len(v.Items))
	visible := false
	for _, i := range v.Items {
		if i.Kind == "activation" {
			continue
		}
		status, owned := scheduled[i.TurnKey]
		if owned && !human[i.TurnKey] {
			if status != "completed" && status != "failed" && status != "interrupted" && status != "cancelled" {
				continue
			}
			if last[i.TurnKey] == api.SilentReminder && !artifacts[i.TurnKey] && status == "completed" {
				continue
			}
			if i.Kind == "assistant" && strings.TrimSpace(i.Text) == api.SilentReminder && len(i.Artifacts) == 0 {
				continue
			}
		}
		out = append(out, i)
		if i.TurnKey == current && (i.Kind == "assistant" && strings.TrimSpace(i.Text) != "" || len(i.Artifacts) > 0) {
			visible = true
		}
	}
	v.Items = out
	v.Quiet = v.Scheduled && (!visible || pending) && v.Connection == "ready" && v.Message == "" && v.Phase != "failed" && v.Phase != "unknown" && v.Phase != "interrupted" && v.Phase != "interrupting"
	for _, a := range v.Approvals {
		if a.Status != "resolved" {
			v.Quiet = false
		}
	}
	if v.Quiet {
		v.Reviews = []api.Review{}
	}
	// CurrentTurn is presentation identity; never an execution target.
	if v.Scheduled && v.CurrentTurn == "" {
		v.CurrentTurn = current
	}
	return v
}
