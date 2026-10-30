package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestContinuitySystemSourcesSurviveNativeDeliveryAndRestart(t *testing.T) {
	sources := []api.SystemSource{api.SystemWorkerResult, api.SystemWorkerApproval, api.SystemWorkerQuestion, api.SystemReminder, api.SystemCallback, api.SystemCommandResult, api.SystemContinuityReview, api.SystemSource("legacy")}
	for _, source := range sources {
		for _, compacted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/compact=%t", source, compacted), func(t *testing.T) {
				posts := 0
				s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
					posts++
					var in wire.PromptRequest
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed", Target: &wire.CommandTarget{TurnId: pointer("source-turn")}})
				})
				s.tools = &api.ToolConnection{}
				s.state.Grants["schedule"] = grantRecord{Grant: wire.SessionBackgroundGrant{Id: "original-grant"}}
				message := api.SystemMessage{ID: "original-system", Source: source, Text: "Source is assigned by the producer, not this text.", Grants: []string{"schedule"}}
				if source == "legacy" {
					message.Source = api.SystemWorkerResult
				}
				if receipt, err := s.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
				// Recreate the older settled journal encoding before reopening it.
				old := s.state.Operations[message.ID]
				old.Body = json.RawMessage("null")
				if source == "legacy" {
					old.SystemSource = ""
				}
				s.state.Operations[message.ID] = old
				if err := s.saveLocked(); err != nil {
					t.Fatal(err)
				}
				restored := New(Options{Directory: filepath.Dir(s.path)})
				restored.client, restored.connected, restored.tools = s.client, true, s.tools
				s = restored
				v := s.state.Views["main"]
				v.CommandCaughtUp = true
				now := time.Now().Round(0)
				emit := func(id, kind string, life *wire.LifecycleEvent, update *json.RawMessage) {
					event := wire.Envelope{Kind: kind, Cursor: pointer("cursor-" + id), SessionId: pointer("main"), TurnId: pointer("source-turn"), Scope: pointer("main"), OccurredAt: &now, Lifecycle: life, Update: update, UsageSemantics: wire.UsageSemanticsProviderUsage}
					if kind == "caelis/notice" {
						event.NoticeKind = wire.NoticeKindCompact
					}
					s.observeContinuity(v, event, now)
					applyEnvelope(v, event)
				}
				usage := json.RawMessage(`{"sessionUpdate":"usage_update","used":"0","size":"0","_meta":{"caelis":{"sdk":{"model":"fixture","provider":"fixture","usage":{"prompt_tokens":"500","cache_read_accounting":"included"}}}}}`)
				for range 2 {
					emit("start", "caelis/lifecycle", &wire.LifecycleEvent{State: "started"}, nil)
					emit("input", "session/update", nil, &usage)
					if compacted {
						emit("compact", "caelis/notice", nil, nil)
					}
					emit("finish", "caelis/lifecycle", &wire.LifecycleEvent{State: "completed"}, nil)
				}
				observed := s.ContinuityObservation()
				want := uint64(1)
				if source == api.SystemContinuityReview || source == "legacy" {
					want = 0
				}
				samples, compactions := int(want), uint64(0)
				if compacted {
					samples = 0
					compactions = want
				}
				if observed.Turns != want || observed.InputCount != samples || observed.Compactions != compactions {
					t.Fatalf("source=%s turns=%d samples=%d want=%d", source, observed.Turns, observed.InputCount, want)
				}
				if receipt, err := s.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
				if posts != 1 {
					t.Fatal("original operation replayed", posts)
				}
			})
		}
	}
}

func TestContinuityCompletionBeforeSystemReceiptCannotSelfCount(t *testing.T) {
	sent, release := make(chan struct{}), make(chan struct{})
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		close(sent)
		<-release
		writeFixture(w, wire.CommandResult{OperationId: "original-review", Outcome: "committed", Target: &wire.CommandTarget{TurnId: pointer("early-turn")}})
	})
	s.tools = &api.ToolConnection{}
	done := make(chan error, 1)
	go func() {
		_, err := s.SubmitSystem(t.Context(), api.SystemMessage{ID: "original-review", Source: api.SystemContinuityReview, Text: "Optional review."})
		done <- err
	}()
	<-sent
	s.mu.Lock()
	v := s.state.Views["main"]
	now := time.Now().Round(0)
	for _, state := range []string{"running", "completed"} {
		e := wire.Envelope{Kind: "caelis/lifecycle", Cursor: pointer("cursor-" + state), Scope: pointer("main"), SessionId: pointer("main"), TurnId: pointer("early-turn"), OccurredAt: &now, Lifecycle: &wire.LifecycleEvent{State: state}}
		s.observeContinuity(v, e, now)
		applyEnvelope(v, e)
	}
	count := s.state.Continuity.Turns
	s.mu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("completion before original prompt receipt counted the maintenance invitation", count)
	}
}
