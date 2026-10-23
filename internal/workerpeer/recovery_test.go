package workerpeer

import (
	"context"
	"errors"
	"net/http/httptest"
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

func TestExplicitRecoveryUsesOnlyAuthenticatedSourceAndEmptyReadiness(t *testing.T) {
	readiness := &recoverableFixtureOwner{fixtureOwner: newFixtureOwner()}
	source := &recoverableFixtureOwner{fixtureOwner: newFixtureOwner()}
	server, err := Open(t.TempDir(), func(_ context.Context, peer, runtime, root string) (Owner, string, *api.WorkExecutionSettings, error) {
		if peer == "readiness" {
			return readiness, "codex", nil, nil
		}
		if peer != "source-a" {
			return nil, "", nil, errors.New("unexpected_source")
		}
		return source, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Enable(true); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(server)
	defer h.Close()
	c := pairFixture(t, server, h, "source-a")
	start := api.WorkStart{ID: "old-worker", TaskStart: api.TaskStart{RequestID: "original-op", Prompt: "fixture"}}
	if _, err = Call(t.Context(), c, Request{Action: "start", Start: start}); err != nil {
		t.Fatal(err)
	}
	source.gone.Store(true)
	readiness.gone.Store(true)
	if _, err = Call(t.Context(), c, Request{Action: "info"}); err == nil {
		t.Fatal("read silently replaced owner")
	}
	if _, err = server.Pairing(t.Context()); err == nil {
		t.Fatal("pairing silently replaced readiness")
	}
	if source.recoveries.Load() != 0 || readiness.recoveries.Load() != 0 {
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
	if source.recoveries.Load() != 1 || readiness.recoveries.Load() != 0 {
		t.Fatal("recovery crossed source")
	}
	if err = server.RecoverReadiness(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Pairing(t.Context()); err != nil {
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
