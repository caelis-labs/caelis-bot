package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestSystemDeadlineAtControlPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		posts         int32
	}{
		{"before-deadline", "accepted", 1},
		{"expired-during-preparation", "rejected", 0},
		{"expired-after-intent", "rejected", 0},
		{"receipt-after-deadline", "accepted", 1},
		{"lost-receipt-after-deadline", "unknown", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := api.SystemMessage{ID: "occurrence:deadline", Source: api.SystemCallback, Text: "Clock", Grants: []string{"schedule:clock"}}
			deadline := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
			var late atomic.Bool
			var posts atomic.Int32
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/control/v1/sessions/main/prompt" {
					http.NotFound(w, r)
					return
				}
				var input wire.PromptRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || value(input.OperationId) != msg.ID {
					t.Errorf("incorrect native prompt identity: %v", err)
				}
				posts.Add(1)
				if tc.name == "receipt-after-deadline" || tc.name == "lost-receipt-after-deadline" {
					late.Store(true)
				}
				if tc.outcome == "unknown" {
					drop(w)
					return
				}
				writeFixture(w, wire.CommandResult{OperationId: msg.ID, Outcome: "committed"})
			})
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			s.tools = &api.ToolConnection{
				PrepareTurn: func(context.Context) error {
					close(entered)
					<-release
					return nil
				},
				PrepareContext: func(context.Context) (api.ContextSeed, error) {
					return api.ContextSeed{Text: "local context"}, nil
				},
			}
			readBinding := func() binding {
				t.Helper()
				raw, err := os.ReadFile(s.path)
				var stored binding
				if err != nil || json.Unmarshal(raw, &stored) != nil {
					t.Errorf("cannot read native journal: %v", err)
				}
				return stored
			}
			var checks int
			ctx := api.WithSystemDispatchDeadline(t.Context(), deadline, func() time.Time {
				checks++
				if tc.name == "expired-after-intent" && checks == 2 {
					// The transport check observes reality after the durable
					// intent, without adding a wall clock delay to the writer.
					if readBinding().Operations[msg.ID].Outcome != "unknown" {
						t.Error("native intent was not durable before HTTP admission")
					}
					late.Store(true)
				}
				if late.Load() {
					return deadline
				}
				return deadline.Add(-time.Second)
			})
			type result struct {
				receipt api.Receipt
				err     error
			}
			done := make(chan result, 1)
			go func() {
				receipt, err := s.SubmitSystem(ctx, msg)
				done <- result{receipt, err}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("PrepareTurn was not reached")
			}
			if posts.Load() != 0 {
				t.Fatal("native prompt overtook preparation")
			}
			if tc.name == "expired-during-preparation" {
				late.Store(true)
			}
			once.Do(func() { close(release) })
			var out result
			select {
			case out = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("native submission did not settle")
			}
			if out.receipt.ID != msg.ID || out.receipt.Outcome != tc.outcome || posts.Load() != tc.posts ||
				out.receipt.Deferred != (tc.posts == 0) || errors.Is(out.err, api.ErrSystemDispatchExpired) != (tc.posts == 0) {
				t.Fatalf("receipt=%+v error=%v posts=%d", out.receipt, out.err, posts.Load())
			}
			if tc.posts == 0 {
				if s.SystemReceipt(msg.ID).Outcome != "unknown" || s.state.Context.Pending != nil || !s.Snapshot().CanSend {
					t.Fatal("an undispatched clock left a native fence")
				}
				if tc.name == "expired-after-intent" {
					stored := readBinding()
					if _, exists := stored.Operations[msg.ID]; exists || stored.Context.Pending != nil {
						t.Fatal("unsent intent or context survived in the journal")
					}
				}
				return
			}
			late.Store(true)
			if receipt, err := s.SubmitSystem(ctx, msg); err != nil || receipt.Outcome != tc.outcome || posts.Load() != 1 {
				t.Fatal("late reconciliation replaced the original operation", receipt, err, posts.Load())
			}
		})
	}
}
