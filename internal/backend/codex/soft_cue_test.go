package codex

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/continuity"
)

func eligibleCues(id string, now time.Time) continuity.State {
	s := continuity.State{}
	s.Bind(id, now.Add(-10*24*time.Hour), now.Add(-10*24*time.Hour))
	for d := range 4 {
		for n := range 2 {
			at := now.Add(time.Duration(d-3)*24*time.Hour - time.Duration(n)*time.Minute)
			// Events must be chronological, including the two turns in a day.
			at = at.Add(time.Duration(2*n-1) * time.Minute)
			s.Observe(fmt.Sprintf("seed-%d-%d", d, n), "fixture", 70_000, at, at)
		}
	}
	return s
}

func TestContinuityPrivateContextPreservesPromptAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		outcome string
		report  bool
	}{{"accepted", false}, {"unknown", false}, {"accepted", true}, {"unknown", true}} {
		t.Run(fmt.Sprintf("%s/report=%v", tc.outcome, tc.report), func(t *testing.T) {
			outcome := tc.outcome
			s, f := sessionPair(t, "")
			s.mu.Lock()
			s.opts.BotTools = &api.ToolConnection{}
			s.binding.Context.Injected = true
			s.binding.Continuity = eligibleCues(s.binding.ThreadID, time.Now().Add(-time.Minute))
			s.binding.Continuity.Configuration = s.continuityConfiguration()
			s.mu.Unlock()
			var captured struct {
				Input   []nativeInput                           `json:"input"`
				Context map[string]struct{ Kind, Value string } `json:"additionalContext"`
			}
			calls := 0
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method != "turn/start" {
					return nil, false
				}
				calls++
				_ = json.Unmarshal(m.Params, &captured)
				if outcome == "unknown" {
					return map[string]any{}, true
				}
				return map[string]any{"turn": nativeTurn{ID: "cue-turn", Status: "completed"}}, true
			}
			f.mu.Unlock()
			prompt := "取消旧授权。\n  保留这些空格与原文。  "
			r, err := s.submitWithSource(testContext(t), api.Submission{ID: "cue-original", Text: prompt}, nil, tc.report, tc.report)
			if err != nil || r.Outcome != outcome {
				t.Fatal(r, err)
			}
			if len(captured.Input) != 1 || captured.Input[0].Text != prompt || captured.Context["caelis-bot-continuity"].Kind != "application" || captured.Context["caelis-bot-continuity"].Value == "" {
				t.Fatal("prompt/context boundary", captured)
			}
			s.mu.Lock()
			state := s.binding.Continuity
			s.mu.Unlock()
			if outcome == "accepted" {
				if state.Accepted.IsZero() || state.Pending != nil || !s.binding.Context.Injected {
					t.Fatal("receipt or first seed changed")
				}
				restored := NewSession(s.opts)
				defer restored.cancelLife()
				if restored.binding.Continuity.Accepted.IsZero() || restored.binding.Continuity.Prepare("after restart", time.Now()) != "" {
					t.Fatal("restart lost cooldown")
				}
			} else {
				if state.Pending == nil || state.Pending.ID != "cue-original" {
					t.Fatal("lost original cue")
				}
				_, _ = s.Submit(testContext(t), api.Submission{ID: "cue-original", Text: prompt}, nil)
				if calls != 1 {
					t.Fatal("unknown prompt redispatched")
				}
				restored := NewSession(s.opts)
				defer restored.cancelLife()
				if restored.binding.Continuity.Pending == nil || restored.binding.Continuity.Prepare("other-after-restart", time.Now()) != "" {
					t.Fatal("unknown identity lost on restart")
				}
				s.mu.Lock()
				s.applyItem(s.binding.ThreadID, "reconciled-turn", nativeItem{ID: "native-user", Type: "userMessage", ClientID: "cue-original"}, true)
				if s.binding.Continuity.Pending != nil || s.binding.Continuity.Accepted.IsZero() {
					t.Error("original native acceptance did not reconcile cue")
				}
				s.mu.Unlock()
			}
		})
	}
}

func TestContinuityCodexPositiveOrdinaryInputsOnly(t *testing.T) {
	s, _ := sessionPair(t, "")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts.BotTools = &api.ToolConnection{}
	s.cueTurn = "ordinary"
	when := time.Now()
	n := Notification{Method: "thread/tokenUsage/updated", ReceivedAt: when, Params: raw(map[string]any{"threadId": s.binding.ThreadID, "turnId": "ordinary", "tokenUsage": map[string]any{"last": map[string]int64{"inputTokens": 70_000, "outputTokens": 3, "totalTokens": 70_003}, "total": map[string]int64{"totalTokens": 70_003}}})}
	s.sampleContinuity(n) // Window size deliberately absent.
	if len(s.binding.Continuity.Windows) != 1 {
		t.Fatal("ordinary counter needs no window")
	}
	s.sampleContinuity(n)
	if len(s.binding.Continuity.Windows[0].Samples) != 1 {
		t.Fatal("duplicate counted")
	}
	s.cueTotal = 0 // A restarted adapter still rejects the persisted counter.
	n.ReceivedAt = time.Now()
	s.sampleContinuity(n)
	if !s.binding.Continuity.Latest.Equal(when) {
		t.Fatal("replayed counter refreshed age")
	}
	s.cueReview = true
	s.cueTotal = 0
	s.sampleContinuity(n)
	if !s.binding.Continuity.Latest.Equal(when) {
		t.Fatal("review input counted")
	}
	s.cueReview = false
	s.cueCompacting = true
	s.binding.Continuity.ForgetTurn("ordinary")
	s.sampleContinuity(n)
	if len(s.binding.Continuity.Windows) != 0 {
		t.Fatal("compact model request counted")
	}
	s.cueCompacting = false
	s.cueTotal = 0
	n.Params = raw(map[string]any{"threadId": s.binding.ThreadID, "turnId": "ordinary", "tokenUsage": map[string]any{"last": map[string]int64{"inputTokens": 0, "totalTokens": 119_000}, "total": map[string]int64{"totalTokens": 900_000}}})
	s.sampleContinuity(n)
	if len(s.binding.Continuity.Windows) != 0 {
		t.Fatal("reestimate counted")
	}
}

func TestContinuityHumanSteerAndModelChangeDoNotCarryCue(t *testing.T) {
	for _, mode := range []string{"human steer", "model change"} {
		t.Run(mode, func(t *testing.T) {
			s, f := sessionPair(t, "")
			s.mu.Lock()
			s.opts.BotTools = &api.ToolConnection{}
			s.binding.Context.Injected = true
			s.binding.Continuity = eligibleCues(s.binding.ThreadID, time.Now().Add(-time.Minute))
			s.binding.Continuity.Configuration = s.continuityConfiguration()
			method := "turn/start"
			if mode == "human steer" {
				s.applyTurn(s.binding.ThreadID, nativeTurn{ID: "active", Status: "inProgress"}, false)
				s.update()
				method = "turn/steer"
			} else {
				s.opts.Execution.Model = "different-model"
			}
			s.mu.Unlock()
			prompt := "撤销旧授权，先做当前任务。\n  "
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method != method {
					return nil, false
				}
				var params struct {
					Input             []nativeInput
					AdditionalContext json.RawMessage
				}
				if err := json.Unmarshal(m.Params, &params); err != nil {
					t.Error(err)
				}
				if len(params.AdditionalContext) != 0 || len(params.Input) != 1 || params.Input[0].Text != prompt {
					t.Error("cue interfered with new instruction/configuration")
				}
				return map[string]any{"turnId": "active", "turn": nativeTurn{ID: "next", Status: "completed"}}, true
			}
			f.mu.Unlock()
			r, err := s.Submit(testContext(t), api.Submission{ID: "priority-original", Text: prompt}, nil)
			if err != nil || r.Outcome != "accepted" {
				t.Fatal(r, err)
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.binding.Continuity.Pending != nil || !s.binding.Continuity.Accepted.IsZero() {
				t.Fatal("suppressed cue consumed cooldown")
			}
		})
	}
}
