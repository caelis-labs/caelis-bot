package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func continuityResident(t *testing.T, engine api.Engine) (*bot.Runtime, *backend.Service) {
	t.Helper()
	root := t.TempDir()
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	r, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureDream(vault, ""); err != nil {
		t.Fatal(err)
	}
	r.Start(engine)
	t.Cleanup(r.Stop)
	service := backend.NewService(engine, func([]string) ([]api.InputFile, error) { return nil, nil }, func([]string) error { return nil }, nil, nil)
	service.SetUserSubmitter(r.SubmitUser)
	service.SetUserActivityObserver(func() func() {
		done, err := r.BeginUserActivity()
		if err != nil {
			t.Error(err)
		}
		return done
	})
	return r, service
}

func TestAcceptedHumanSteeringNeverGetsInterruptedByStaleDream(t *testing.T) {
	for _, captured := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-capture", true: "after-capture"}[captured], func(t *testing.T) {
			s, f := sessionPair(t, "block")
			s.mu.Lock()
			s.run, s.lastTurn = "run-native", "run-native"
			s.runs[s.run] = "inProgress"
			s.state.Phase = "working"
			s.binding.Context.Injected = true
			s.update()
			s.mu.Unlock()
			var steering, interrupts atomic.Int32
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "turn/steer" {
					steering.Add(1)
				}
				if m.Method == "turn/interrupt" {
					interrupts.Add(1)
				}
				return nil, false
			}
			f.mu.Unlock()
			r, service := continuityResident(t, s)
			call := api.ToolInvocation{Provider: "codex", CallID: "stale-dream", Session: "thread-native", Turn: "run-native"}
			dream := func() api.ToolResult {
				return r.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Old goal; original review: Notebook/review.md, lines 30 onward unread."}`))
			}
			var result api.ToolResult
			if captured {
				result = dream()
				if result.IsError || !result.TurnComplete {
					t.Fatal(result)
				}
			}
			input := api.Submission{ID: "human-correction", Text: "Stop that plan. Keep the original records and wait for my review."}
			if receipt, err := service.Submit(t.Context(), input); err != nil || receipt.Outcome != "accepted" {
				t.Fatal(receipt, err)
			}
			if captured {
				err := s.CompleteToolTurn(api.WithRenewalGuard(t.Context(), result.CheckTurnComplete), call)
				if !errors.Is(err, api.ErrConversationRenewalDeferred) {
					t.Fatal("stale terminal result was not rejected", err)
				}
			} else {
				result = dream()
				if !result.IsError || result.TurnComplete {
					t.Fatal("superseded Dream remained terminal", result)
				}
			}
			if steering.Load() != 1 || interrupts.Load() != 0 || s.ConversationState().Session != call.Session {
				t.Fatal("human message replayed or interrupted", steering.Load(), interrupts.Load())
			}
			f.mu.Lock()
			inputJSON := string(f.lastParams["input"])
			f.mu.Unlock()
			var parts []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(inputJSON), &parts); err != nil || len(parts) != 1 || parts[0].Text != input.Text {
				t.Fatal("human text changed", inputJSON, err)
			}
		})
	}
}

// Pause after the last successful slow guard, before the native binding lock.
// All subsequent work still passes through the real adapter and resident.
type pausedContinuitySession struct {
	*Session
	checked, release chan struct{}
}

func (s *pausedContinuitySession) RenewAfterTool(ctx context.Context, call api.ToolInvocation) (string, error) {
	original := ctx
	checks := 0
	ctx = api.WithRenewalGuard(ctx, func() error {
		err := api.CheckRenewalGuard(original)
		checks++
		if checks == 2 && err == nil {
			close(s.checked)
			select {
			case <-s.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return err
	})
	return s.Session.RenewAfterTool(ctx, call)
}

func TestHumanIngressAfterLastReadCannotBeOvertakenByNativeAdoption(t *testing.T) {
	for _, action := range []string{"correction", "stop"} {
		t.Run(action, func(t *testing.T) {
			s, f := sessionPair(t, "normal")
			s.mu.Lock()
			s.run = ""
			s.lastTurn = "terminal"
			s.runs["terminal"] = "interrupted"
			s.binding.Context.Injected = true
			s.update()
			s.mu.Unlock()
			var creates atomic.Int32
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/start" {
					creates.Add(1)
					return map[string]any{"thread": nativeThread{ID: "unused"}, "model": "native-default"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			wrapped := &pausedContinuitySession{Session: s, checked: make(chan struct{}), release: make(chan struct{})}
			r, service := continuityResident(t, wrapped)
			call := api.ToolInvocation{Provider: "codex", CallID: "original-dream", Session: "thread-native", Turn: "terminal"}
			result := r.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original task and authorization; review note path and unread scope remain pending."}`))
			if result.IsError || !result.TurnComplete {
				t.Fatal(result)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			renewed := make(chan error, 1)
			go func() { renewed <- r.RenewPendingToolHandoff(ctx) }()
			select {
			case <-wrapped.checked:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			registered := make(chan struct{}, 2)
			service.SetUserActivityObserver(func() func() {
				done, err := r.BeginUserActivity()
				if err != nil {
					t.Error(err)
				}
				registered <- struct{}{}
				return done
			})
			human := make(chan error, 1)
			go func() {
				if action == "stop" {
					human <- service.Interrupt(ctx)
					return
				}
				receipt, err := service.Submit(ctx, api.Submission{ID: "correction", Text: "Keep the old work; wait for my review."})
				if err == nil && receipt.Outcome != "accepted" {
					err = errors.New("correction not accepted")
				}
				human <- err
			}()
			select {
			case <-registered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			close(wrapped.release)
			if err := <-renewed; err != nil {
				t.Fatal(err)
			}
			if err := <-human; err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			binding := s.binding
			s.mu.Unlock()
			if binding.ThreadID != call.Session || creates.Load() != 1 || len(binding.DeferredToolRenewals) != 1 || binding.DeferredToolRenewals[0].NewThread != "unused" {
				t.Fatal("stale read overtook human ingress or lost original receipt", binding, creates.Load())
			}
		})
	}
}

type adoptedContinuitySession struct {
	*Session
	adopted, release chan struct{}
}

func (s *adoptedContinuitySession) RenewAfterTool(ctx context.Context, call api.ToolInvocation) (string, error) {
	id, err := s.Session.RenewAfterTool(ctx, call)
	if err == nil {
		close(s.adopted)
		select {
		case <-s.release:
		case <-ctx.Done():
			return id, ctx.Err()
		}
	}
	return id, err
}
func TestFirstSystemInputAfterNativeUnlockConsumesCompleteHandoffOnce(t *testing.T) {
	s, f := sessionPair(t, "normal")
	s.mu.Lock()
	s.run = ""
	s.lastTurn = "terminal"
	s.runs["terminal"] = "interrupted"
	s.binding.Context.Injected = true
	s.update()
	s.mu.Unlock()
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/start" {
			return map[string]any{"thread": nativeThread{ID: "new-native"}, "model": "native-default"}, true
		}
		if m.Method == "turn/start" {
			return map[string]any{"turn": nativeTurn{ID: "first-new-turn", Status: "inProgress"}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	wrapped := &adoptedContinuitySession{Session: s, adopted: make(chan struct{}), release: make(chan struct{})}
	resident, _ := continuityResident(t, wrapped)
	seeds := make(chan api.ContextSeed, 1)
	s.opts.BotTools = &api.ToolConnection{PrepareContext: func(context.Context) (api.ContextSeed, error) {
		seed := resident.PrepareHandoffContext()
		seed.Text = "[Bot context: core identity]\n" + seed.Text
		seeds <- seed
		return seed, nil
	}, ConsumeContext: resident.ConsumeHandoffContext}
	call := api.ToolInvocation{Provider: "codex", CallID: "original-dream", Session: "thread-native", Turn: "terminal"}
	out := resident.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Task-123 still pending; authorization is read-only. Notebook/review.md lines 30 onward remain unread."}`))
	if out.IsError || !out.TurnComplete {
		t.Fatal(out)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	renewed := make(chan error, 1)
	go func() { renewed <- resident.RenewPendingToolHandoff(ctx) }()
	select {
	case <-wrapped.adopted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	receipt, err := resident.SubmitSystem(ctx, api.SystemMessage{ID: "first-approval", Source: api.SystemWorkerApproval, Text: "Original Worker approval requires attention."})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	seed := <-seeds
	if seed.HandoffDigest == "" || !strings.Contains(seed.Text, "Notebook/review.md lines 30") || !strings.Contains(seed.Text, "core identity") {
		t.Fatal("native input became available before handoff", seed)
	}
	close(wrapped.release)
	if err := <-renewed; err != nil {
		t.Fatal(err)
	}
	if resident.PrepareHandoffContext().HandoffDigest != "" {
		t.Fatal("renewal return resurrected an already consumed handoff")
	}
	out = resident.CallTool(ctx, "bot_schedule", json.RawMessage(`{"request":{"type":"context"}}`))
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), "context_handoff") {
		t.Fatal("first system input left Bot tools permanently fenced")
	}
}
