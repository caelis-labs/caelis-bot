package caelis

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Only exact live resident events can establish freshness. Context gauges and
// billable provider usage have distinct semantics, and may arrive in either order.
// A restored gauge alone never implies a recent model call.
func applyLiveUsage(v *view, e wire.Envelope, now time.Time) {
	key := value(e.ProjectionId)
	if key == "" {
		key = value(e.EventId)
	}
	if key == "" || v.Seen[key] || value(e.SessionId) != v.State.SessionId || value(e.TurnId) == "" || e.ParentTool != nil || value(e.ApprovalRequestId) != "" || (value(e.Scope) != "" && value(e.Scope) != "main") {
		return
	}
	if e.OccurredAt == nil || e.OccurredAt.After(now) || now.Sub(*e.OccurredAt) > 45*time.Second {
		return
	}
	var update wire.ACPUsageUpdate
	if e.Update == nil || json.Unmarshal(*e.Update, &update) != nil || update.SessionUpdate != "usage_update" {
		return
	}
	turn := value(e.TurnId)
	switch e.UsageSemantics {
	case wire.UsageSemanticsProviderUsage:
		if v.ModelTurn == turn && !e.OccurredAt.After(v.Usage.ModelAt) {
			return
		}
		v.ModelTurn, v.Usage.ModelAt = turn, e.OccurredAt.Round(0)
	case wire.UsageSemanticsContextGauge:
		used, eu := strconv.ParseInt(string(update.Used), 10, 64)
		window, ew := strconv.ParseInt(string(update.Size), 10, 64)
		v.UsageTurn = turn
		v.Usage.Used, v.Usage.Window = 0, 0
		if eu == nil && ew == nil && used >= 0 && window > 0 {
			v.Usage.Used, v.Usage.Window = used, window
		}
	}
}
