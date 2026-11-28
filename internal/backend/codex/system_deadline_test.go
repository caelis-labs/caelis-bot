package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestSystemDeadlineAtTurnWrite(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		writes        int32
	}{
		{"before-deadline", "accepted", 1},
		{"expired-during-preparation", "rejected", 0},
		{"expired-waiting-for-writer", "rejected", 0},
		{"receipt-after-deadline", "accepted", 1},
		{"lost-receipt-after-deadline", "unknown", 1},
		{"capacity-rejection-at-deadline", "rejected", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := sessionPair(t, "early-terminal")
			msg := api.SystemMessage{ID: "occurrence:deadline", Source: api.SystemCallback, Text: "Clock", OriginRequests: []string{"registration"}}
			deadline := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
			var late atomic.Bool
			var writes atomic.Int32
			posted := make(chan struct{}, 2)
			holdResponse := make(chan struct{})
			defer close(holdResponse)
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method != "turn/start" {
					return nil, false
				}
				var input struct {
					ID string `json:"clientUserMessageId"`
				}
				if err := json.Unmarshal(m.Params, &input); err != nil || input.ID != msg.ID {
					t.Errorf("wrong native turn identity: %v", err)
				}
				writes.Add(1)
				posted <- struct{}{}
				if tc.name == "capacity-rejection-at-deadline" {
					late.Store(true)
					return &NativeError{Code: -32000, Message: "selected model is at capacity."}, true
				}
				if tc.name == "receipt-after-deadline" {
					late.Store(true)
				}
				if tc.outcome == "unknown" {
					<-holdResponse
				}
				return map[string]any{"turn": nativeTurn{ID: "native-clock", Status: "inProgress"}}, true
			}
			f.mu.Unlock()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			s.mu.Lock()
			s.opts.BotTools = &api.ToolConnection{
				PrepareTurn: func(context.Context) error {
					close(entered)
					<-release
					return nil
				},
				PrepareContext: func(context.Context) (api.ContextSeed, error) {
					return api.ContextSeed{Text: "local context"}, nil
				},
			}
			s.capacityBackoff = func(int) time.Duration { return 0 }
			token := s.client.rpc.writeToken
			s.mu.Unlock()
			if tc.name == "expired-waiting-for-writer" {
				<-token
			}
			var unlock sync.Once
			if tc.name == "expired-waiting-for-writer" {
				defer unlock.Do(func() { token <- struct{}{} })
			}
			now := func() time.Time {
				if late.Load() {
					return deadline
				}
				return deadline.Add(-time.Second)
			}
			type result struct {
				receipt api.Receipt
				err     error
			}
			done := make(chan result, 1)
			go func() {
				receipt, err := s.SubmitSystem(api.WithSystemDispatchDeadline(parent, deadline, now), msg)
				done <- result{receipt, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("PrepareTurn was not reached")
			}
			if writes.Load() != 0 {
				t.Fatal("native write overtook preparation")
			}
			if tc.name == "expired-during-preparation" {
				late.Store(true)
			}
			once.Do(func() { close(release) })
			if tc.name == "expired-waiting-for-writer" {
				waitForDeadlineIntent(t, s, msg.ID)
				late.Store(true)
				unlock.Do(func() { token <- struct{}{} })
			}
			if tc.outcome == "unknown" {
				select {
				case <-posted:
				case <-time.After(5 * time.Second):
					t.Fatal("original frame did not reach the native fixture")
				}
				late.Store(true)
				cancel()
			}
			var out result
			select {
			case out = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("native submission did not settle")
			}
			if out.receipt.ID != msg.ID || out.receipt.Outcome != tc.outcome || writes.Load() != tc.writes ||
				out.receipt.Deferred != (tc.writes == 0) || errors.Is(out.err, api.ErrSystemDispatchExpired) != (tc.writes == 0) {
				t.Fatalf("receipt=%+v error=%v writes=%d", out.receipt, out.err, writes.Load())
			}
			if tc.writes == 0 {
				stored := readDeadlineBinding(t, s)
				if stored.Pending != nil || stored.Context.Pending != nil || stored.HostReports[msg.ID] ||
					len(stored.Scheduled) != 0 || len(stored.ScheduledOrigins) != 0 || !s.Snapshot().CanSend {
					t.Fatal("undispatched clock left a durable native fence")
				}
				return
			}
			late.Store(true)
			if receipt, err := s.SubmitSystem(api.WithSystemDispatchDeadline(t.Context(), deadline, now), msg); err != nil || receipt.Outcome != tc.outcome || writes.Load() != 1 {
				t.Fatal("late reconciliation replaced the original operation", receipt, err, writes.Load())
			}
		})
	}
}

func readDeadlineBinding(t *testing.T, s *Session) binding {
	t.Helper()
	raw, err := os.ReadFile(s.opts.StateFile)
	var stored binding
	if err != nil || json.Unmarshal(raw, &stored) != nil {
		t.Fatalf("cannot read native journal: %v", err)
	}
	return stored
}

func waitForDeadlineIntent(t *testing.T, s *Session, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		s.mu.Lock()
		pending, changed := s.binding.Pending, s.changed
		s.mu.Unlock()
		if pending != nil && pending.ID == id {
			if stored := readDeadlineBinding(t, s); stored.Pending == nil || stored.Pending.ID != id {
				t.Fatal("writer was held before the intent became durable")
			}
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("native write intent did not reach the held writer")
		}
	}
}
