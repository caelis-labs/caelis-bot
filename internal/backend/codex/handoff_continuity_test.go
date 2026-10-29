package codex

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRenewalDeferralRetainsSourceAndUnusedNativeReceipt(t *testing.T) {
	for _, duringCreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-create", true: "during-create"}[duringCreate], func(t *testing.T) {
			s, f := sessionPair(t, "normal")
			s.mu.Lock()
			s.lastTurn, s.run = "terminal", ""
			s.runs["terminal"], s.state.CanSend = "interrupted", true
			s.binding.Context.Injected = true
			s.mu.Unlock()
			var allowed atomic.Bool
			allowed.Store(duringCreate)
			var starts atomic.Int32
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/start" {
					starts.Add(1)
					allowed.Store(false) // Human correction or failed memory recheck.
					return map[string]any{"thread": nativeThread{ID: "unused-native"}, "model": "native-default"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			ctx := api.WithRenewalGuard(t.Context(), func() error {
				if !allowed.Load() {
					return api.ErrConversationRenewalDeferred
				}
				return nil
			})
			call := api.ToolInvocation{Provider: "codex", CallID: "original", Session: "thread-native", Turn: "terminal"}
			if _, err := s.RenewAfterTool(ctx, call); !errors.Is(err, api.ErrConversationRenewalDeferred) {
				t.Fatal("renewal did not defer", err)
			}
			restored := NewSession(s.opts)
			if s.binding.ThreadID != call.Session || !s.binding.Context.Injected || !s.Snapshot().CanSend || s.binding.ToolRenewal != nil {
				t.Fatal("source binding or its injected memory changed", s.binding)
			}
			if !duringCreate {
				if starts.Load() != 0 {
					t.Fatal("created despite initial refusal")
				}
				return
			}
			if len(restored.binding.DeferredToolRenewals) != 1 || restored.binding.DeferredToolRenewals[0].NewThread != "unused-native" || restored.binding.DeferredToolRenewals[0].CallID != call.CallID {
				t.Fatal("original unused native receipt was lost", restored.binding.DeferredToolRenewals)
			}
			allowed.Store(true)
			if _, err := s.RenewAfterTool(ctx, call); !errors.Is(err, api.ErrConversationRenewalDeferred) || starts.Load() != 1 {
				t.Fatal("superseded original was adopted or replayed", err, starts.Load())
			}
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/start" {
					starts.Add(1)
					return map[string]any{"thread": nativeThread{ID: "next-native"}, "model": "native-default"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			call.CallID = "later-distinct-dream"
			if sid, err := s.RenewAfterTool(ctx, call); err != nil || sid != "next-native" || starts.Load() != 2 {
				t.Fatal("original receipt blocked a later explicit Dream", sid, err, starts.Load())
			}
		})
	}
}
