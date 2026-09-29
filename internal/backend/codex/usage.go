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
				Total  int64 `json:"totalTokens"`
				Input  int64 `json:"inputTokens"`
				Output int64 `json:"outputTokens"`
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
		return
	}
	s.usageTotal = u.Total.Tokens
	if u.Window == nil || *u.Window <= 0 || u.Response.Input <= 0 || u.Response.Output < 0 || u.Response.Total < u.Response.Input {
		s.usage = api.ContextUsage{}
		return
	}
	when := event.ReceivedAt
	if when.IsZero() {
		return
	} // An un-timestamped fixture/replay is not live evidence.
	s.usage = api.ContextUsage{Used: u.Response.Total, Window: *u.Window, ModelAt: when}
	// Receive time is an upper bound when a server supplies no native timestamp.
	// A supplied timestamp includes time spent asleep or queued in the transport.
	if event.EmittedAtMS > 0 {
		s.usage.ModelAt = time.UnixMilli(event.EmittedAtMS)
	}
}
