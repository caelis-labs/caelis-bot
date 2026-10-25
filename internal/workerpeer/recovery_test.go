package workerpeer

import (
	"context"
	"errors"
	"net/http/httptest"
	"runtime/trace"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type recoverableFixtureOwner struct {
	*fixtureOwner
	gone       atomic.Bool
	recoveries atomic.Int32
}

func (o *recoverableFixtureOwner) Connect(context.Context) error {
	if o.gone.Load() {
		return errors.New("confirmed_original_owner_exited")
	}
	return nil
}
func (o *recoverableFixtureOwner) RecoverNewWork(context.Context) error {
	o.recoveries.Add(1)
	o.gone.Store(false)
	return nil
}

func TestExplicitRecoveryUsesOnlyAuthenticatedSourceWithoutReadinessOwner(t *testing.T) {
	source := &recoverableFixtureOwner{fixtureOwner: newFixtureOwner()}
	var factoryCalls atomic.Int32
	server, err := Open(t.TempDir(), func(_ context.Context, peer, runtime, root string) (Owner, string, *api.WorkExecutionSettings, error) {
		factoryCalls.Add(1)
		if peer != "source-a" {
			return nil, "", nil, errors.New("unexpected_source")
		}
		return source, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(server)
	defer h.Close()
	c := pairFixture(t, server, h, "source-a")
	start := api.WorkStart{ID: "old-worker", TaskStart: api.TaskStart{RequestID: "original-op", Prompt: "fixture"}}
	if _, err = Call(t.Context(), c, Request{Action: "start", Start: start}); err != nil {
		t.Fatal(err)
	}
	initialRecoveries := source.recoveries.Load()
	source.gone.Store(true)
	if _, err = Call(t.Context(), c, Request{Action: "info"}); err == nil {
		t.Fatal("read silently replaced owner")
	}
	before := factoryCalls.Load()
	if err = server.PairingReadiness(t.Context()); err != nil || factoryCalls.Load() != before {
		t.Fatal("pairing created an empty owner", err)
	}
	if source.recoveries.Load() != initialRecoveries { // Initial authenticated pairing prepares its own source.
		t.Fatal("ordinary read recovered owner")
	}
	bad := c
	bad.Token = "wrong-proof"
	if _, err = Call(t.Context(), bad, Request{Action: "recover"}); err == nil {
		t.Fatal("unauthenticated recovery")
	}
	if _, err = Call(t.Context(), c, Request{Action: "recover"}); err != nil {
		t.Fatal(err)
	}
	if source.recoveries.Load() != initialRecoveries+1 {
		t.Fatal("recovery crossed source")
	}
	if err = server.PairingReadiness(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = server.PairingReadiness(t.Context()); err != nil {
		t.Fatal("empty readiness stayed pinned", err)
	}
	if source.starts != 1 || len(source.tasks) != 1 || source.tasks["old-worker"].Task.ID != "old-worker" {
		t.Fatal("recovery dispatched or replaced work")
	}
	if err = server.Revoke("source-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = Call(t.Context(), c, Request{Action: "recover"}); err == nil {
		t.Fatal("revoked source recovered")
	}
}

func TestNewPreparationRecoversOnlyNewWorkAndKeepsOriginalReceipt(t *testing.T) {
	source := &recoverableFixtureOwner{fixtureOwner: newFixtureOwner()}
	server, err := Open(t.TempDir(), func(_ context.Context, peer, runtime, root string) (Owner, string, *api.WorkExecutionSettings, error) {
		if peer != "source-a" {
			return nil, "", nil, errors.New("unexpected_source")
		}
		return source, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(server)
	defer h.Close()
	c := pairFixture(t, server, h, "source-a")
	start := api.WorkStart{ID: "old-worker", TaskStart: api.TaskStart{RequestID: "original-unknown", Prompt: "original"}}
	trace.WithRegion(t.Context(), "recovery-original-start", func() {
		_, err = Call(t.Context(), c, Request{Action: "start", Start: start})
	})
	if err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	op := source.ops[start.RequestID]
	op.Outcome = "unknown"
	source.ops[start.RequestID] = op
	source.mu.Unlock()
	source.gone.Store(true)
	before := source.recoveries.Load()
	if _, err = Call(t.Context(), c, Request{Action: "read", ID: start.ID}); err == nil || source.recoveries.Load() != before {
		t.Fatal("old task read recovered/replaced execution", err)
	}
	if _, err = Call(t.Context(), c, Request{Action: "prepare", ID: "new-worker"}); err != nil {
		t.Fatal("new-work preparation could not recover confirmed exit", err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.recoveries.Load() != before+1 || source.starts != 1 || source.ops[start.RequestID].Outcome != "unknown" || len(source.tasks) != 1 || source.tasks[start.ID].Task.ID != start.ID {
		t.Fatal("new preparation migrated/replayed old work")
	}
}
func TestPairingReadyProbeIsBoundedWithoutNativeOwner(t *testing.T) {
	s, err := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		t.Fatal("readiness created owner")
		return nil, "", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	s.ConfigureReadiness(func(ctx context.Context) error { probes.Add(1); <-ctx.Done(); return ctx.Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = s.PairingReadiness(ctx); !errors.Is(err, context.Canceled) || probes.Load() != 1 {
		t.Fatal(err)
	}
}
