package codex

import (
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Accept live provider-style counters, using cumulative total only to reject
// duplicate notifications. App Server also emits local re-estimates with zero
// input counters; those are not model usage or a measured post-compaction baseline.
// Historical threads and worker usage are excluded.
func (s *Session) applyUsage(event Notification) {
	var n struct {
		Thread string `json:"threadId"`
		Turn   string `json:"turnId"`
		Usage  struct {
			Response struct {
				Total      int64  `json:"totalTokens"`
				Input      int64  `json:"inputTokens"`
				Output     int64  `json:"outputTokens"`
				CacheRead  *int64 `json:"cachedInputTokens"`
				CacheWrite *int64 `json:"cacheWriteInputTokens"`
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
	var cacheRead, cacheWrite int64
	if u.Response.CacheRead != nil {
		cacheRead = *u.Response.CacheRead
	}
	if u.Response.CacheWrite != nil {
		cacheWrite = *u.Response.CacheWrite
	}
	if u.Total.Tokens <= s.usageTotal {
		if s.usage.ModelAt.IsZero() {
			s.usageReason = "nonincreasing_total"
		}
		return
	}
	s.usageTotal = u.Total.Tokens
	if u.Window == nil || *u.Window <= 0 || u.Response.Input <= 0 || u.Response.Output < 0 || u.Response.Total < u.Response.Input || cacheRead < 0 || cacheWrite < 0 || cacheRead > u.Response.Input || cacheWrite > u.Response.Input {
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
		CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite,
		CacheReadTokensReported: u.Response.CacheRead != nil, CacheWriteTokensReported: u.Response.CacheWrite != nil}
	s.usageReason = "live_token_usage"
	// Receive time is an upper bound when a server supplies no native timestamp.
	// A supplied timestamp includes time spent asleep or queued in the transport.
	if event.EmittedAtMS > 0 {
		s.usage.ModelAt = time.UnixMilli(event.EmittedAtMS)
	}
}
