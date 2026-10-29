package caelis

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
)

func (s *Session) ContinuityObservation() continuity.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.state.Views[s.state.Session.SessionId]
	if !s.connected || s.closed || v == nil || !v.CommandCaughtUp || s.state.Continuity.Session != s.state.Session.SessionId {
		return continuity.Observation{}
	}
	return s.state.Continuity.Observation
}

// Exact, caught-up events only. Persistence shares the existing projection
// checkpoint/cursor; replacing or replaying history never samples it again.
func (s *Session) observeContinuity(v *view, e wire.Envelope, now time.Time) {
	key := value(e.ProjectionId)
	if key == "" {
		key = value(e.EventId)
	}
	turn := value(e.TurnId)
	if s.tools == nil || key == "" || v.Seen[key] || value(e.SessionId) != s.state.Session.SessionId || turn == "" || e.ParentTool != nil || value(e.ApprovalRequestId) != "" || value(e.Scope) != "main" || e.OccurredAt == nil || e.OccurredAt.After(now) || now.Sub(*e.OccurredAt) > time.Minute {
		return
	}
	c := &s.state.Continuity
	if e.Kind == "caelis/lifecycle" && e.Lifecycle != nil {
		switch e.Lifecycle.State {
		case "started", "running":
			if !retiredRunHead(v, turn) && v.Turns[turn] != "completed" {
				c.Start(value(e.SessionId), turn, *e.OccurredAt)
			}
		case "completed", "failed", "cancelled", "interrupted", "stopped":
			for _, operation := range s.state.Operations {
				if operation.TurnID == turn && operation.hostNotice() {
					c.Active = ""
					return
				}
			}
			c.Finish(turn, e.Lifecycle.State == "completed", *e.OccurredAt)
		}
	}
	if e.Kind == "caelis/notice" && e.NoticeKind == wire.NoticeKindCompact {
		c.Compact(turn, key)
	}
	if e.UsageSemantics != wire.UsageSemanticsProviderUsage || e.Update == nil {
		return
	}
	var update wire.ACPUsageUpdate
	if json.Unmarshal(*e.Update, &update) != nil || update.SessionUpdate != "usage_update" || update.Meta == nil {
		return
	}
	meta, _ := (*update.Meta)["caelis"].(map[string]any)
	sdk, _ := meta["sdk"].(map[string]any)
	usage, _ := sdk["usage"].(map[string]any)
	model, _ := sdk["model"].(string)
	provider, _ := sdk["provider"].(string)
	basis, _ := usage["cache_read_accounting"].(string)
	input, ok := continuityCounter(usage["prompt_tokens"])
	if !ok || model == "" || provider == "" {
		return
	}
	switch basis {
	case "included":
	case "separate":
		cached, reported := continuityCounter(usage["cached_input_tokens"])
		if !reported || cached == 0 && usage["cached_input_tokens_reported"] != true || cached > math.MaxInt64-input {
			return
		}
		input += cached
	default:
		return
	}
	c.Sample(turn, "caelis:full_input:"+provider+":"+model+":"+s.state.Session.Profile.Version, input)
}

func continuityCounter(raw any) (int64, bool) {
	switch v := raw.(type) {
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil && n >= 0
	case float64:
		if v >= 0 && v < 1<<53 && math.Trunc(v) == v {
			return int64(v), true
		}
	}
	return 0, false
}
