package codex

import (
	"errors"
	"fmt"
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

// Fail only the final binding commit, after thread/start and its original ID
// have been saved. A new human Turn must not skip cleanup of that known create.
func TestOriginalRenewalSettlesBeforeNewTurnAdmission(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprint("restart=", restart), func(t *testing.T) {
			s, f := sessionPair(t, "normal")
			s.mu.Lock()
			s.lastTurn = "terminal"
			s.run = ""
			s.runs["terminal"] = "interrupted"
			s.state.CanSend = true
			s.mu.Unlock()
			var starts atomic.Int32
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/start" {
					n := starts.Add(1)
					return map[string]any{"thread": nativeThread{ID: fmt.Sprint("created-", n)}, "model": "native-default"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			path := s.opts.StateFile
			invalid := t.TempDir()
			ctx := api.WithRenewalCommit(t.Context(), func(_ string, adopt func() error) error {
				s.opts.StateFile = invalid
				defer func() { s.opts.StateFile = path }()
				return adopt()
			})
			original := api.ToolInvocation{Provider: "codex", CallID: "A", Session: "thread-native", Turn: "terminal"}
			if _, err := s.RenewAfterTool(ctx, original); err == nil {
				t.Fatal("binding failure was not injected")
			}
			if s.binding.ThreadID != original.Session || s.binding.ToolRenewal == nil || s.binding.ToolRenewal.NewThread != "created-1" {
				t.Fatal("lost original create", s.binding)
			}
			if restart {
				restored := NewSession(s.opts)
				// Reuse the native connection, as Connect would, without starting a second runtime.
				s.mu.Lock()
				s.binding = restored.binding
				s.mu.Unlock()
			}
			s.mu.Lock()
			s.lastTurn = "human-new-turn"
			s.runs[s.lastTurn] = "interrupted"
			s.mu.Unlock()
			if _, err := s.RenewAfterTool(t.Context(), original); !errors.Is(err, api.ErrConversationRenewalDeferred) {
				t.Fatal("original known create not finalized", err)
			}
			restored := NewSession(s.opts)
			if restored.binding.ToolRenewal != nil || len(restored.binding.DeferredToolRenewals) != 1 || restored.binding.DeferredToolRenewals[0].NewThread != "created-1" {
				t.Fatal("residual renewal", restored.binding)
			}
			next := original
			next.CallID = "B"
			next.Turn = "human-new-turn"
			if id, err := s.RenewAfterTool(t.Context(), next); err != nil || id != "created-2" || starts.Load() != 2 {
				t.Fatal("next Dream blocked or original create replayed", id, err, starts.Load())
			}
		})
	}
}
