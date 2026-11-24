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
			if result.IsError || result.CheckTurnComplete == nil {
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
