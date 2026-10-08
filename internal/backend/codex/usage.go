package codex

import (
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The last model response estimates the current context; total is used only to
// reject duplicate notifications. Historical threads and worker usage are excluded.
func (s *Session) applyUsage(event Notification) {
	var n struct {
		Thread string `json:"threadId"`
		Turn   string `json:"turnId"`
		Usage  struct {
			Response struct {
				Total      int64 `json:"totalTokens"`
				Input      int64 `json:"inputTokens"`
				Output     int64 `json:"outputTokens"`
				CacheRead  int64 `json:"cachedInputTokens"`
				CacheWrite int64 `json:"cacheWriteInputTokens"`
			} `json:"last"`
			Total struct {
				Tokens int64 `json:"totalTokens"`
			} `json:"total"`
			Window *int64 `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if !s.decodeEvent(event, &n, false) || n.Thread != s.binding.ThreadID || n.Turn == "" || n.Turn != s.usageTurn || n.Turn != s.lastTurn {
		return
	}
	u := n.Usage
	if u.Total.Tokens <= s.usageTotal {
		if s.usage.ModelAt.IsZero() {
			s.usageReason = "nonincreasing_total"
		}
		return
	}
	s.usageTotal = u.Total.Tokens
	if u.Window == nil || *u.Window <= 0 || u.Response.Input <= 0 || u.Response.Output < 0 || u.Response.Total < u.Response.Input || u.Response.CacheRead < 0 || u.Response.CacheWrite < 0 || u.Response.CacheRead > u.Response.Input || u.Response.CacheWrite > u.Response.Input {
		s.usage = api.ContextUsage{}
		s.usageReason = "invalid_token_usage"
		return
	}
	when := event.ReceivedAt
	if when.IsZero() {
		s.usageReason = "missing_live_timestamp"
		return
	} // An un-timestamped fixture/replay is not live evidence.
	s.usage = api.ContextUsage{Used: u.Response.Total, Window: *u.Window, ModelAt: when,
		InputTokens: u.Response.Input, OutputTokens: u.Response.Output,
		CacheReadTokens: u.Response.CacheRead, CacheWriteTokens: u.Response.CacheWrite}
	s.usageReason = "live_token_usage"
	// Receive time is an upper bound when a server supplies no native timestamp.
	// A supplied timestamp includes time spent asleep or queued in the transport.
	if event.EmittedAtMS > 0 {
		s.usage.ModelAt = time.UnixMilli(event.EmittedAtMS)
	}
}
