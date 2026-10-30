package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func continuityResident(t *testing.T, engine api.Engine) (*bot.Runtime, *backend.Service) {
	t.Helper()
	root := t.TempDir()
	vault, err := notebook.OpenVault(filepath.Join(root, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vault.Close() })
	r, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
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

func TestCallbackAfterAcceptedHumanSteeringIsNonterminalAndNotReplayed(t *testing.T) {
	call := wire.SessionToolCall{Id: "dream-original", CallId: "provider-dream", SessionId: "main", PrincipalId: "owner", TurnId: "turn", ItemId: "item", Name: "bot_dream", ToolsVersion: "v1", ConfigurationRevision: "1", Source: wire.SessionSource{Kind: "user", OperationId: "prompt"}, State: "pending", Arguments: map[string]any{"handoff": "Original task; Notebook/review.md lines 30 onward remain unread. Wait for user review before acting."}}
	var steers, effects, results atomic.Int32
	var receipts []wire.SessionToolCallResult
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/claim"):
			claimed := call
			claimed.State = "claimed"
			writeFixture(w, claimed)
		case strings.HasSuffix(r.URL.Path, "/steer"):
			steers.Add(1)
			var in wire.SteerRequest
			_ = json.NewDecoder(r.Body).Decode(&in)
			if value(in.Input) != "Keep the original work; wait for my review." || in.Target.TurnId != "turn" {
				t.Error("changed original correction or turn", in)
			}
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted"})
		case strings.HasSuffix(r.URL.Path, "/result"):
			results.Add(1)
			var out wire.SessionToolCallResult
			_ = json.NewDecoder(r.Body).Decode(&out)
			receipts = append(receipts, out)
			writeFixture(w, struct{}{})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	v := s.state.Views["main"]
	v.State.Run.Active = pointer(true)
	v.State.Run.TurnId = pointer("turn")
	v.State.Run.HandleId = pointer("handle")
	v.State.Run.RunId = pointer("run")
	s.state.Context.Injected = true
	r, service := continuityResident(t, s)
	host := &acceptanceTools{call: func(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
		effects.Add(1)
		out := r.CallTool(ctx, name, args)
		if out.IsError || !out.TurnComplete {
			t.Error("initial Dream was not terminal", out)
		}
		receipt, err := service.Submit(ctx, api.Submission{ID: "correction", Text: "Keep the original work; wait for my review."})
		if err != nil || receipt.Outcome != "accepted" {
			t.Error(receipt, err)
		}
		return out
	}}
	s.catalogs = map[string]map[string]api.ApplicationTools{"v1": {"bot_dream": host}}
	s.state.Operations["prompt"] = journal{Path: "/sessions/main/prompt", Outcome: "accepted", Source: call.Source}
	for range 2 {
		if err := s.handleCall(t.Context(), s.client, s.state.Session, s.state.PrincipalID, call); err != nil {
			t.Fatal(err)
		}
	}
	if effects.Load() != 1 || steers.Load() != 1 || results.Load() != 2 {
		t.Fatal("replayed original effect or input", effects.Load(), steers.Load(), results.Load())
	}
	for _, receipt := range receipts {
		if receipt.Outcome != "failed" || value(receipt.TurnComplete) {
			t.Fatal("stale Dream terminated accepted steering", receipt)
		}
	}
}

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
		if checks == 3 && err == nil {
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
			s, creates, call := continuityFixture(t, nil, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/control/v1/sessions/main/prompt" {
					var in wire.PromptRequest
					_ = json.NewDecoder(r.Body).Decode(&in)
					writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted"})
					return
				}
				t.Errorf("unexpected request %s", r.URL.Path)
				http.NotFound(w, r)
			})
			wrapped := &pausedContinuitySession{Session: s, checked: make(chan struct{}), release: make(chan struct{})}
			resident, service := continuityResident(t, wrapped)
			result := resident.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original task and authorization; review note path and unread scope remain pending."}`))
			if result.IsError || !result.TurnComplete {
				t.Fatal(result)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			renewed := make(chan error, 1)
			go func() { renewed <- resident.RenewPendingToolHandoff(ctx) }()
			select {
			case <-wrapped.checked:
			case err := <-renewed:
				t.Fatalf("renewal returned before the final guard: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			registered := make(chan struct{}, 1)
			service.SetUserActivityObserver(func() func() {
				done, err := resident.BeginUserActivity()
				if err != nil {
					t.Error(err)
				}
				registered <- struct{}{}
				return done
			})
			human := make(chan error, 1)
			go func() {
				if action == "stop" {
					err := service.Interrupt(ctx)
					if err != nil && strings.Contains(err.Error(), "当前没有可停止") {
						err = nil
					}
					human <- err
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
			sid := s.state.Session.SessionId
			j := s.state.Operations["tool-renew-"+digest([]byte(call.CallID))]
			s.mu.Unlock()
			if sid != call.Session || creates.Load() != 1 || j.Resource != "next" || !j.RenewalDeferred {
				t.Fatal("stale read overtook human ingress or lost original receipt", sid, j, creates.Load())
			}
		})
	}
}
