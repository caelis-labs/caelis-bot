package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestConnectionLostAuthStreamReconcilesOriginalReceipt(t *testing.T) {
	var starts, reads atomic.Int32
	var outcome atomic.Value
	outcome.Store("unknown")
	var operation atomic.Value
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if runtimeFixtureHandshake(w, r) {
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/control/v1")
		switch {
		case path == "/completion/slash-arguments":
			writeFixture(w, []wire.SlashArgCandidate{{Value: "fixture", Display: pointer("Fixture")}})
		case path == "/configuration/connect-model":
			starts.Add(1)
			var request wire.ConnectModelRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			operation.Store(value(request.OperationId))
			w.Header().Set("Content-Type", "text/event-stream")
			raw, _ := json.Marshal(map[string]any{"operation_id": value(request.OperationId), "sequence": "1", "phase": "waiting_for_browser", "verification_url": "https://example.invalid/device", "user_code": "TEST-ONLY"})
			fmt.Fprintf(w, "event: model_authentication\ndata: %s\n\n", raw)
			w.(http.Flusher).Flush() // EOF loses the result; never replay this POST.
		case strings.HasPrefix(path, "/operations/"):
			reads.Add(1)
			if r.Method != "GET" || strings.TrimPrefix(path, "/operations/") != operation.Load().(string) {
				t.Error("wrong original receipt query")
			}
			if outcome.Load().(string) == "missing" {
				http.NotFound(w, r)
				return
			}
			op := operation.Load().(string)
			result := outcome.Load().(string)
			if result == "wrong-owner" {
				op = "other-operation"
				result = "committed"
			}
			writeFixture(w, wire.CommandResult{OperationId: op, Outcome: wire.Outcome(result)})
		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
		}
	})
	s := &Connections{}
	defer s.Close()
	v, err := s.Start(t.Context(), settings, api.RuntimeConnectionInput{Kind: "account", Choice: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	v = waitConnection(t, s, v.ID, "models")
	_, err = s.Advance(t.Context(), api.RuntimeFlowAction{ID: v.ID, Revision: v.Revision, Action: "connect", Input: api.RuntimeFlowInput{Model: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	v = waitConnection(t, s, v.ID, "unknown")
	if v.Authorization == nil || v.Authorization.UserCode != "TEST-ONLY" || v.Authorization.CanSubmit {
		t.Fatal("lost last challenge or authorized replay")
	}
	for _, result := range []string{"unknown", "accepted", "missing", "wrong-owner"} {
		outcome.Store(result)
		next, e := s.Wait(t.Context(), v.ID, -1)
		if e != nil || next.Stage != "unknown" || next.Authorization == nil {
			t.Fatalf("uncertainty erased for %s", result)
		}
	}
	outcome.Store("committed")
	final, err := s.Wait(t.Context(), v.ID, -1)
	if err != nil || final.Stage != "complete" || final.ID != v.ID || final.Authorization != nil {
		t.Fatal("did not reconcile original committed receipt")
	}
	if starts.Load() != 1 || reads.Load() != 5 {
		t.Fatalf("wrong effects/reads %d/%d", starts.Load(), reads.Load())
	}
}

func TestConnectionExpiryDistinguishesDeadlineFromUnknownOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f := &connectionFlow{ctx: ctx, cancel: cancel, pending: true, changed: make(chan struct{}), view: api.RuntimeFlow{ID: "original", Stage: "authorization", Authorization: &api.RuntimeAuthorization{UserCode: "TEST-ONLY", ExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}}}
	v := f.snapshot()
	if v.Stage != "unknown" || v.Reason != "expired" || v.Authorization != nil {
		t.Fatal("expiry claimed result or retained expired challenge")
	}
	f.view.Stage = "unknown"
	f.view.Authorization = &api.RuntimeAuthorization{UserCode: "TEST-ONLY", ExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339)}
	if f.snapshot().Authorization != nil {
		t.Fatal("old unknown challenge leaked after deadline")
	}
}

func TestConnectionReconcileDoesNotCompleteAgentPreparation(t *testing.T) {
	f := &connectionFlow{request: api.RuntimeConnectionInput{Kind: "agent"}, operation: "prepare-original", client: &client{}, view: api.RuntimeFlow{ID: "agent-original", Stage: "unknown"}}
	// ACP has intermediate committed preparation commands. An account-only
	// reconciliation must neither read them as a connection nor claim completion.
	f.reconcile(t.Context())
	if f.view.Stage != "unknown" {
		t.Fatal("intermediate agent operation marked connected")
	}
}
