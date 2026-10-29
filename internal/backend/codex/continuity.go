package codex

import (
	"encoding/json"
	"time"
)

func (s *Session) bindContinuity(thread nativeThread) {
	var created time.Time
	if thread.CreatedAt > 0 {
		created = time.Unix(thread.CreatedAt, 0)
	}
	s.binding.Continuity.Bind(thread.ID, created, time.Now())
	s.cueTurn, s.cueTotal, s.cueCompacting = "", 0, false
	s.cueReview = false
}

func (s *Session) continuityConfiguration() string {
	model := s.opts.Execution.Model
	if model == "" {
		model = s.residentExecution.Model
	}
	if model == "" {
		return ""
	}
	return "codex:input_includes_cache:" + model
}

func (s *Session) sampleContinuity(event Notification) {
	if s.opts.BotTools == nil || s.cueTurn == "" || s.cueCompacting || s.cueReview {
		return
	}
	var n struct {
		Thread string `json:"threadId"`
		Turn   string `json:"turnId"`
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
	if json.Unmarshal(event.Params, &n) != nil || n.Thread != s.binding.ThreadID || n.Turn != s.cueTurn || n.Usage.Last.Input <= 0 || n.Usage.Last.Output < 0 || n.Usage.Last.Total < n.Usage.Last.Input || n.Usage.Total.Tokens <= s.cueTotal || event.ReceivedAt.IsZero() {
		return
	}
	s.cueTotal = n.Usage.Total.Tokens
	configuration := s.continuityConfiguration()
	if configuration == "" {
		return
	}
	s.binding.Continuity.Observe(n.Turn, configuration, n.Usage.Last.Input, event.ReceivedAt, time.Now(), n.Usage.Total.Tokens)
	_ = s.save() // Sampling failure must not interrupt ordinary work.
}
