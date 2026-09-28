package care

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func reconcile(t *testing.T, e *Engine, now time.Time, results map[string]api.BackgroundResult) {
	t.Helper()
	if err := e.Deliver(t.Context(), now, Presence{}, false, noReceipt, accepted, func(id string) api.BackgroundResult { return results[id] }); err != nil {
		t.Fatal(err)
	}
}
func TestSilentCareDoesNotConsumeInterruptionBudget(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	if err := e.Configure(t.Context(), Policy{2, 0}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		now := instant.Add(time.Duration(i) * time.Minute)
		receive(t, e, now)
		if err := e.Deliver(t.Context(), now, presence(), true, noReceipt, accepted); err != nil {
			t.Fatal(err)
		}
		state := e.Snapshot()
		a := state.Activations[len(state.Activations)-1]
		if a.Status != "accepted" {
			t.Fatal(a)
		}
		reconcile(t, e, now, map[string]api.BackgroundResult{a.ID: {ID: a.ID, Complete: true}})
	}
	b := e.Budget(instant.Add(12 * time.Minute))
	if b.InterruptionsUsed != 0 || b.Reserved != 0 || b.Remaining != 2 || len(e.Snapshot().Attempts) != 12 {
		t.Fatal(b)
	}
}
func TestUnknownReservationIsPerRuleAndNeverExpiresIntoRetry(t *testing.T) {
	e := opened(t)
	save(t, e, rule("first"))
	save(t, e, rule("other"))
	receive(t, e, instant)
	sends := []string{}
	send := func(_ context.Context, a Activation) (api.Receipt, error) {
		sends = append(sends, a.RuleID)
		return api.Receipt{ID: a.ID, Outcome: "unknown"}, nil
	}
	if err := e.Deliver(t.Context(), instant, presence(), true, noReceipt, send); err != nil {
		t.Fatal(err)
	}
	if err := e.Deliver(t.Context(), instant.Add(5*time.Minute), presence(), true, noReceipt, send); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sends, []string{"first", "other"}) {
		t.Fatal(sends)
	}
	e, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	now := instant.Add(48 * time.Hour)
	receive(t, e, now)
	if err := e.Deliver(t.Context(), now, presence(), true, noReceipt, send); err != nil {
		t.Fatal(err)
	}
	if len(sends) != 2 || e.Budget(now).Reserved != 2 {
		t.Fatal("unknown retried/released", sends, e.Budget(now))
	}
}
func TestVisibleResultIsCountedOnceAcrossRestartAndLateReceipt(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	receive(t, e, instant)
	_ = e.Deliver(t.Context(), instant, presence(), true, noReceipt, func(_ context.Context, a Activation) (api.Receipt, error) {
		return api.Receipt{ID: a.ID, Outcome: "unknown"}, nil
	})
	a := e.Snapshot().Activations[0]
	results := map[string]api.BackgroundResult{a.ID: {ID: a.ID, Visible: true, ObservedAt: instant}}
	reconcile(t, e, instant, results)
	if b := e.Budget(instant); b.InterruptionsUsed != 1 || b.Reserved != 0 {
		t.Fatal(b)
	}
	e, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	results[a.ID] = api.BackgroundResult{ID: a.ID, Visible: true, Complete: true, ObservedAt: instant}
	reconcile(t, e, instant.Add(time.Minute), results)
	reconcile(t, e, instant.Add(time.Hour), results)
	if b := e.Budget(instant.Add(time.Hour)); b.InterruptionsUsed != 1 || b.Reserved != 0 {
		t.Fatal(b)
	}
	if e.Snapshot().Activations[0].Result != "visible" {
		t.Fatal("approval was refunded")
	}
}
func TestExpiredAndRejectedCareDoesNotStartFullCooldown(t *testing.T) {
	e := opened(t)
	r := rule("x")
	r.CooldownSeconds = 86400
	r.ExpiresSeconds = 60
	save(t, e, r)
	receive(t, e, instant)
	reconcile(t, e, instant.Add(time.Minute), nil)
	if !e.Snapshot().Rules[0].Last.IsZero() {
		t.Fatal("queued work charged cooldown")
	}
	now := instant.Add(2 * time.Minute)
	receive(t, e, now)
	_ = e.Deliver(t.Context(), now, presence(), true, noReceipt, func(_ context.Context, a Activation) (api.Receipt, error) {
		return api.Receipt{ID: a.ID, Outcome: "rejected"}, nil
	})
	if !e.Snapshot().Rules[0].Last.IsZero() || e.Budget(now).Reserved != 0 {
		t.Fatal("rejection retained cooldown/budget")
	}
	receive(t, e, now.Add(time.Second))
	if len(e.Snapshot().Activations) != 3 {
		t.Fatal("rejected rule suppressed")
	}
}
func TestCarePolicyAndMigrationPreserveIdentity(t *testing.T) {
	e := opened(t)
	r := save(t, e, rule("x"))
	receive(t, e, instant)
	old := e.Snapshot()
	old.Version = 1
	old.Attempts = []time.Time{instant}
	old.Activations[0].Status = "unknown"
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(e.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	e, err = Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	got := e.Snapshot()
	if got.Version != 2 || !reflect.DeepEqual(got.Rules[0], r) || got.Activations[0].ID != old.Activations[0].ID || got.Activations[0].Status != "unknown" {
		t.Fatal(got)
	}
	if b := e.Budget(instant); b.MigrationReservations != 1 || b.Reserved != 1 || b.InterruptionsUsed != 0 {
		t.Fatal(b)
	}
	if b := e.Budget(instant.Add(25 * time.Hour)); b.MigrationReservations != 0 || b.Reserved != 1 {
		t.Fatal(b)
	}
	deny := func(context.Context, string, string) error { return errors.New("denied") }
	if err := e.Configure(t.Context(), Policy{2, 1}, deny); err == nil || e.Snapshot().Policy != DefaultPolicy() {
		t.Fatal("unapproved policy applied")
	}
	if err := e.Configure(t.Context(), Policy{2, 1}, nil); err != nil {
		t.Fatal(err)
	}
	e, err = Open(e.path)
	if err != nil || e.Snapshot().Policy != (Policy{2, 1}) {
		t.Fatal(err)
	}
	for _, p := range []Policy{{0, 1}, {257, 1}, {8, -1}} {
		if e.Configure(t.Context(), p, nil) == nil {
			t.Fatal("invalid policy")
		}
	}
}
func TestAccountingWriteFailureCannotFreeReservation(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	receive(t, e, instant)
	_ = e.Deliver(t.Context(), instant, presence(), true, noReceipt, accepted)
	a := e.Snapshot().Activations[0]
	e.write = func(string, any) error { return errors.New("full") }
	if e.Deliver(t.Context(), instant, Presence{}, false, noReceipt, accepted, func(id string) api.BackgroundResult { return api.BackgroundResult{ID: id, Complete: true} }) == nil {
		t.Fatal("lost storage error")
	}
	e, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if e.Budget(instant).Reserved != 1 || e.Snapshot().Activations[0].ID != a.ID {
		t.Fatal("lost uncertain reservation")
	}
}

func TestReservedCapacityReleasedOnlyByExactNativeResult(t *testing.T) {
	e := opened(t)
	if err := e.Configure(t.Context(), Policy{1, 0}, nil); err != nil {
		t.Fatal(err)
	}
	save(t, e, rule("first"))
	save(t, e, rule("second"))
	receive(t, e, instant)
	count := 0
	send := func(_ context.Context, a Activation) (api.Receipt, error) {
		count++
		return api.Receipt{ID: a.ID, Outcome: "unknown"}, nil
	}
	_ = e.Deliver(t.Context(), instant, presence(), true, noReceipt, send)
	first := e.Snapshot().Activations[0]
	_ = e.Deliver(t.Context(), instant, presence(), true, noReceipt, send, func(id string) api.BackgroundResult { return api.BackgroundResult{ID: "wrong", Complete: true} })
	if count != 1 || e.Budget(instant).Remaining != 0 {
		t.Fatal("reserved capacity exceeded")
	}
	_ = e.Deliver(t.Context(), instant, presence(), true, noReceipt, send, func(id string) api.BackgroundResult {
		if id == first.ID {
			return api.BackgroundResult{ID: id, Complete: true}
		}
		return api.BackgroundResult{ID: id}
	})
	if count != 2 || e.Budget(instant).Reserved != 1 {
		t.Fatal("silent result failed to release capacity")
	}
}
