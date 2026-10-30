package codex

import (
	"encoding/json"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
)

func (s *Session) ContinuityObservation() continuity.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bound || s.loading || s.closed || s.binding.Continuity.Session != s.binding.ThreadID {
		return continuity.Observation{}
	}
	return s.binding.Continuity.Observation
}

// Called before projection, so terminal/native item identities still provide
// duplicate fences. No transcript scan or additional Runtime request is needed.
func (s *Session) observeContinuity(event Notification) {
	if s.opts.BotTools == nil || s.loading || event.ReceivedAt.IsZero() {
		return
	}
	switch event.Method {
	case "turn/started", "turn/completed", "item/started", "item/completed", "thread/tokenUsage/updated":
	default:
		return
	}
	var n struct {
		Thread string     `json:"threadId"`
		TurnID string     `json:"turnId"`
		Turn   nativeTurn `json:"turn"`
		Item   nativeItem `json:"item"`
		Usage  struct {
			Last struct {
				Input  int64 `json:"inputTokens"`
				Output int64 `json:"outputTokens"`
				Total  int64 `json:"totalTokens"`
			} `json:"last"`
			Total struct {
				Tokens int64 `json:"totalTokens"`
			} `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(event.Params, &n) != nil || n.Thread != s.binding.ThreadID {
		return
	}
	now := event.ReceivedAt
	if event.EmittedAtMS > 0 {
		now = time.UnixMilli(event.EmittedAtMS)
	}
	if now.After(event.ReceivedAt) || event.ReceivedAt.Sub(now) > time.Minute {
		return
	}
	c := &s.binding.Continuity
	switch event.Method {
	case "turn/started":
		if !terminal(s.runs[n.Turn.ID]) {
			c.Start(n.Thread, n.Turn.ID, now)
		}
	case "turn/completed":
		if !s.continuityBusinessTurn(n.Turn.ID) {
			if c.Active == n.Turn.ID {
				c.Active = ""
			}
			return
		}
		if c.Finish(n.Turn.ID, n.Turn.Status == "completed", now) {
			_ = s.save()
		}
	case "item/started", "item/completed":
		if n.Item.Type == "enteredReviewMode" {
			c.Active = ""
		}
		if event.Method == "item/completed" && n.Item.Type == "contextCompaction" && n.Item.Status != "failed" {
			if item := s.nativeItems[opaque(n.TurnID, n.Item.ID)]; item.Status != "completed" {
				c.Compact(n.TurnID, n.Item.ID)
			}
		}
	case "thread/tokenUsage/updated":
		if c.Active != n.TurnID || n.Usage.Last.Input <= 0 || n.Usage.Last.Output < 0 || n.Usage.Last.Total < n.Usage.Last.Input || n.Usage.Total.Tokens <= c.Total {
			return
		}
		c.Total = n.Usage.Total.Tokens
		model := s.opts.Execution.Model
		if model == "" {
			model = s.residentExecution.Model
		}
		if model != "" {
			c.Sample(n.TurnID, "codex:full_input:"+model+":"+s.binding.RuntimeVersion, n.Usage.Last.Input)
		}
	}
}

func (s *Session) continuityBusinessTurn(turn string) bool {
	for _, report := range s.binding.HostReportTurns {
		if report.Thread == s.binding.ThreadID && report.Turn == turn {
			return report.SystemSource.BusinessWake()
		}
	}
	for _, scheduled := range s.binding.Scheduled {
		if scheduled == turn {
			return false
		}
	}
	// A completion can beat the prompt receipt/correlation. Do not guess that
	// an as-yet unbound host input is human work, especially our own invitation.
	if p := s.binding.Pending; p != nil && s.binding.HostReports[p.ID] {
		return false
	}
	return true
}
