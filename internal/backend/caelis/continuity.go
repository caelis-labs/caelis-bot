package caelis

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Caelis has no per-submission application context port. Collect evidence only;
// never substitute a user content part or rewrite an original prompt for a cue.
func (s *Session) sampleContinuity(v *view, e wire.Envelope, now time.Time) {
	key := value(e.ProjectionId)
	if key == "" {
		key = value(e.EventId)
	}
	if s.tools == nil || key == "" || v.Seen[key] || value(e.SessionId) != s.state.Session.SessionId || value(e.TurnId) == "" || e.ParentTool != nil || value(e.ApprovalRequestId) != "" || value(e.Scope) != "main" || e.UsageSemantics != wire.UsageSemanticsProviderUsage || e.OccurredAt == nil || e.Update == nil {
		return
	}
	var update wire.ACPUsageUpdate
	if json.Unmarshal(*e.Update, &update) != nil || update.SessionUpdate != "usage_update" || update.Meta == nil {
		return
	}
	// Public wire normalization retains native SDK metadata on the usage update,
	// not the envelope's reduced _meta object.
	caelis, _ := (*update.Meta)["caelis"].(map[string]any)
	sdk, _ := caelis["sdk"].(map[string]any)
	usage, _ := sdk["usage"].(map[string]any)
	model, _ := sdk["model"].(string)
	provider, _ := sdk["provider"].(string)
	basis, _ := usage["cache_read_accounting"].(string)
	input, ok := inputCounter(usage["prompt_tokens"])
	if !ok || model == "" || provider == "" {
		return
	}
	switch basis {
	case "included":
	case "separate":
		cached, reported := inputCounter(usage["cached_input_tokens"])
		if !reported || cached == 0 && usage["cached_input_tokens_reported"] != true || cached > math.MaxInt64-input {
			return
		}
		input += cached
	default:
		return // Unknown accounting is not comparable to full input.
	}
	s.state.Continuity.Bind(s.state.Session.SessionId, time.Time{}, now)
	s.state.Continuity.Observe(value(e.TurnId), "caelis:input_includes_cache:"+provider+":"+model, input, *e.OccurredAt, now)
}

func inputCounter(raw any) (int64, bool) {
	var text string
	switch v := raw.(type) {
	case string:
		text = v
	case json.Number:
		text = string(v)
	case float64:
		if v < 0 || v >= 1<<53 || math.Trunc(v) != v {
			return 0, false
		}
		return int64(v), true
	default:
		return 0, false
	}
	n, err := strconv.ParseInt(text, 10, 64)
	return n, err == nil && n >= 0
}
