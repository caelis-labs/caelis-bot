package caelis

import (
	"encoding/json"
	"math"
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
		applyProviderCounters(v, update.Meta)
		v.UsageEvidence = "awaiting_context_gauge"
		if v.UsageTurn == turn {
			if v.Usage.Window > 0 {
				v.UsageEvidence = "live_gauge_and_provider"
			} else {
				v.UsageEvidence = "invalid_context_gauge"
			}
		}
		if update.Cost != nil && update.Cost.Total != nil && !math.IsNaN(*update.Cost.Total) && !math.IsInf(*update.Cost.Total, 0) && *update.Cost.Total >= 0 {
			v.Usage.LastProviderCost, v.Usage.LastProviderCostKnown = *update.Cost.Total, true
			v.Usage.LastProviderCostCurrency = ""
			if update.Cost.Currency != nil && len(*update.Cost.Currency) == 3 {
				currency := *update.Cost.Currency
				if currency[0] >= 'A' && currency[0] <= 'Z' && currency[1] >= 'A' && currency[1] <= 'Z' && currency[2] >= 'A' && currency[2] <= 'Z' {
					v.Usage.LastProviderCostCurrency = currency
				}
			}
		} else {
			v.Usage.LastProviderCost, v.Usage.LastProviderCostKnown, v.Usage.LastProviderCostCurrency = 0, false, ""
		}
	case wire.UsageSemanticsContextGauge:
		used, eu := strconv.ParseInt(string(update.Used), 10, 64)
		window, ew := strconv.ParseInt(string(update.Size), 10, 64)
		v.UsageTurn = turn
		v.Usage.Used, v.Usage.Window = 0, 0
		v.UsageEvidence = "invalid_context_gauge"
		if eu == nil && ew == nil && used >= 0 && window > 0 {
			v.Usage.Used, v.Usage.Window = used, window
			v.UsageEvidence = "awaiting_provider_usage"
			if v.ModelTurn == turn && !v.Usage.ModelAt.IsZero() {
				v.UsageEvidence = "live_gauge_and_provider"
			}
		}
	}
}

// Core's envelope metadata uses decimal strings, unlike the numeric status
// snapshot. Keep each reported counter independent; never infer cost or totals.
func applyProviderCounters(v *view, meta *wire.ACPMetadata) {
	var usage map[string]any
	if meta != nil {
		if caelis, ok := (*meta)["caelis"].(map[string]any); ok {
			usage, _ = caelis["usage"].(map[string]any)
		}
	}
	counter := func(key string) (int64, bool) {
		raw, ok := usage[key].(string)
		if !ok {
			return 0, false
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return n, true
	}
	cache := func(key string) (int64, bool) {
		n, valid := counter(key)
		// Older Hosts did not publish a presence flag. A nonzero reported
		// value still carries evidence; zero requires explicit presence.
		return n, valid && (n > 0 || usage[key+"_reported"] == true)
	}
	v.Usage.InputTokens, _ = counter("prompt_tokens")
	v.Usage.OutputTokens, _ = counter("completion_tokens")
	v.Usage.CacheReadTokens, v.Usage.CacheReadTokensReported = cache("cached_input_tokens")
	v.Usage.CacheWriteTokens, v.Usage.CacheWriteTokensReported = cache("cache_write_tokens")
	v.Usage.CacheWrite5mTokens, v.Usage.CacheWrite5mTokensReported = cache("cache_write_5m_tokens")
	v.Usage.CacheWrite1hTokens, v.Usage.CacheWrite1hTokensReported = cache("cache_write_1h_tokens")
}
