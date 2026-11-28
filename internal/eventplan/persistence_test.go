package eventplan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func obstructLedger(t *testing.T, path string) func() {
	t.Helper()
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".saved", path); err != nil {
			t.Fatal(err)
		}
	}
}

func requireRenameFailure(t *testing.T, err error) {
	t.Helper()
	var failure *localstate.WriteError
	if !errors.As(err, &failure) || failure.Stage != "rename" || failure.Completion != nil {
		t.Fatalf("expected a confirmed rename failure, got %v", err)
	}
}

func TestConfirmedFailureRollsBackAndIndependentEventRuns(t *testing.T) {
	var launches atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		launches.Add(1)
		return json.RawMessage(`{"count":1}`), nil
	})
	before := e.Snapshot()
	restore := obstructLedger(t, e.path)
	_, err := saveTestPlan(e, Plan{ID: "failed", Event: "mail", Kind: "after", Seconds: 1}, *now)
	requireRenameFailure(t, err)
	if got := e.Snapshot(); !reflect.DeepEqual(before, got) {
		t.Fatalf("failed save published state: %+v", got)
	}
	restore()
	p, err := saveTestPlan(e, Plan{ID: "independent", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}

	restore = obstructLedger(t, e.path)
	requireRenameFailure(t, e.Tick(t.Context(), p.Next, noDelivery()))
	if launches.Load() != 0 || len(e.Snapshot().Occurrences) != 0 {
		t.Fatal("failed intent published an occurrence or invoked a collector")
	}
	restore()
	collect(t, e, p.Next, noDelivery())
	got := e.Snapshot()
	if launches.Load() != 1 || len(got.Occurrences) != 1 || got.Occurrences[0].Plan != p.ID || got.Occurrences[0].Status != "pending" {
		t.Fatalf("independent event did not collect once: %+v", got.Occurrences)
	}
}

// Delay the first writer's reply while the ordered-write tests use the actual
// filesystem writer. localstate separately exercises its real deadline path.
func delayLedgerWrite(e *Engine) chan struct{} {
	release := make(chan struct{})
	e.write = func(path string, value any) error {
		e.write = localstate.Write
		done := make(chan localstate.WriteCompletion, 1)
		go func() {
			<-release
			done <- localstate.WriteCompletion{Err: localstate.Write(path, value)}
		}()
		return &localstate.WriteError{Stage: "sync", Err: context.DeadlineExceeded, Completion: done}
	}
	return release
}

func settleDelayedLedger(t *testing.T, e *Engine, release chan struct{}, wantStage string) {
	t.Helper()
	var failure *localstate.WriteError
	if err := e.FirstStorageFailure(); !errors.As(err, &failure) || failure.Completion == nil {
		t.Fatalf("first failure has no diagnostic completion: %v", err)
	}
	close(release)
	select {
	case result := <-failure.Completion:
		if wantStage == "" {
			if result.Err != nil {
				t.Fatal(result.Err)
			}
		} else if stage := localstate.DescribeFailure(result.Err).Stage; stage != wantStage {
			t.Fatalf("final stage = %q, want %q", stage, wantStage)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original write did not settle")
	}
}

func TestLateMutationSerializesOtherWriters(t *testing.T) {
	for _, outcome := range []string{"committed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			e, now := setup(t, nil)
			release := delayLedgerWrite(e)
			original := Plan{ID: "original", Event: "mail", Kind: "after", Seconds: 1}
			if _, err := saveTestPlan(e, original, *now); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("original deadline = %v", err)
			}
			next := Plan{ID: "independent", Event: "mail", Kind: "after", Seconds: 2}
			concurrent := make(chan error, 1)
			go func() { _, err := saveTestPlan(e, next, *now); concurrent <- err }()
			if err := <-concurrent; !errors.Is(err, ErrWritePending) {
				t.Fatalf("concurrent write overtook original: %v", err)
			}
			if len(e.Snapshot().Plans) != 0 || !e.NextCheck(*now).IsZero() {
				t.Fatal("published an unconfirmed write or polled for it")
			}
			if outcome == "failed" {
				restore := obstructLedger(t, e.path)
				settleDelayedLedger(t, e, release, "rename")
				restore()
			} else {
				settleDelayedLedger(t, e, release, "")
				if _, err := saveTestPlan(e, original, now.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := saveTestPlan(e, next, *now); err != nil {
				t.Fatal(err)
			}
			onDisk, err := Open(e.path, nil, *now)
			if err != nil {
				t.Fatal(err)
			}
			defer onDisk.Close()
			plans := onDisk.Snapshot().Plans
			if outcome == "committed" {
				if len(plans) != 2 || plans[0].ID != original.ID || !plans[0].Next.Equal(now.Add(time.Second)) {
					t.Fatalf("late commit missing or duplicated: %+v", plans)
				}
				plans = plans[1:]
			}
			if len(plans) != 1 || plans[0].ID != next.ID {
				t.Fatalf("independent write missing: %+v", plans)
			}
		})
	}
}

func TestLateIntentIsClearedBeforeFreshWakeAdmission(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		return json.RawMessage(`{"count":1}`), nil
	})
	p, err := saveTestPlan(e, Plan{ID: "wake", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, e, p.Next, noDelivery())
	original := e.Snapshot().Occurrences[0].ID
	d := noDelivery()
	d.Ready = func() bool { return true }
	calls := 0
	d.Submit = func(_ context.Context, w Wake) (string, error) {
		if w.ID != original {
			t.Fatalf("original wake identity changed: %s", w.ID)
		}
		calls++
		return "accepted", nil
	}
	release := delayLedgerWrite(e)
	if err := e.Tick(t.Context(), p.Next, d); !errors.Is(err, context.DeadlineExceeded) || calls != 0 {
		t.Fatalf("unconfirmed intent submitted: %v, calls=%d", err, calls)
	}
	settleDelayedLedger(t, e, release, "")
	d.Ready = func() bool { return false }
	tick(t, e, p.Next, d)
	restored, err := Open(e.path, nil, p.Next)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	s := restored.Snapshot()
	if s.Occurrences[0].Status != "pending" || calls != 0 {
		t.Fatalf("unused intent consumed wake admission: %+v, calls=%d", s, calls)
	}
	d.Ready = func() bool { return true }
	tick(t, e, p.Next, d)
	tick(t, e, p.Next, d)
	if calls != 1 || e.Snapshot().Occurrences[0].Status != "completed" {
		t.Fatalf("original wake did not submit once: calls=%d", calls)
	}
}

func TestFailedObservationCheckpointDoesNotInvokeActionsAgain(t *testing.T) {
	for _, outcome := range []string{"accepted", "unknown", "deferred"} {
		t.Run(outcome, func(t *testing.T) {
			var collections atomic.Int32
			e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
				collections.Add(1)
				return json.RawMessage(`{"count":1}`), nil
			})
			p, err := saveTestPlan(e, Plan{ID: "observe", Event: "mail", Kind: "after", Seconds: 1}, *now)
			if err != nil {
				t.Fatal(err)
			}
			tick(t, e, p.Next, noDelivery())
			e.workers.Wait()
			restore := obstructLedger(t, e.path)
			requireRenameFailure(t, e.Tick(t.Context(), p.Next, noDelivery()))
			if e.Snapshot().Occurrences[0].Status != "collecting" {
				t.Fatal("lost original collection intent")
			}
			restore()
			tick(t, e, p.Next, noDelivery())
			original := e.Snapshot().Occurrences[0].ID
			d := noDelivery()
			d.Ready = func() bool { return true }
			calls := 0
			d.Submit = func(_ context.Context, w Wake) (string, error) {
				if w.ID != original || calls != 0 {
					t.Fatalf("unexpected delivery: %s or duplicate (%d)", w.ID, calls)
				}
				calls++
				restore = obstructLedger(t, e.path)
				return outcome, nil
			}
			requireRenameFailure(t, e.Tick(t.Context(), p.Next, d))
			if e.Snapshot().Occurrences[0].Status != "dispatching" {
				t.Fatal("lost original dispatch intent")
			}
			restore()
			d.Ready = func() bool { return outcome != "deferred" }
			tick(t, e, p.Next, d)
			tick(t, e, p.Next, d)
			s := e.Snapshot()
			want := map[string]string{"accepted": "completed", "unknown": "unknown_wake", "deferred": "pending"}[outcome]
			if calls != 1 || collections.Load() != 1 || s.Occurrences[0].Status != want {
				t.Fatalf("original effects changed: %+v, calls=%d collections=%d", s, calls, collections.Load())
			}
		})
	}
}

func TestLateWakeReceiptKeepsOriginalAndAllowsIndependentWake(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		return json.RawMessage(`{"count":1}`), nil
	})
	p, err := saveTestPlan(e, Plan{ID: "observe", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, e, p.Next, noDelivery())
	original := e.Snapshot().Occurrences[0].ID
	d := noDelivery()
	d.Ready = func() bool { return true }
	var release chan struct{}
	firstCalls := 0
	d.Submit = func(_ context.Context, w Wake) (string, error) {
		firstCalls++
		if w.ID != original || firstCalls != 1 {
			t.Fatalf("unexpected activation %s, calls=%d", w.ID, firstCalls)
		}
		release = delayLedgerWrite(e)
		return "unknown", errors.New("fixture lost receipt")
	}
	if err := e.Tick(t.Context(), p.Next, d); !errors.Is(err, context.DeadlineExceeded) || firstCalls != 1 {
		t.Fatalf("late wake receipt: %v, calls=%d", err, firstCalls)
	}
	if got := e.Snapshot().Occurrences[0].Status; got != "dispatching" {
		t.Fatalf("unpublished receipt overwrote its fence: %s", got)
	}
	settleDelayedLedger(t, e, release, "")
	originalReceipt := e.Snapshot().Occurrences[0]
	if originalReceipt.ID != original || originalReceipt.Status != "unknown_wake" {
		t.Fatalf("lost original receipt: %+v", originalReceipt)
	}

	independent, err := saveTestPlan(e, Plan{ID: "independent", Kind: "after", Seconds: 2}, p.Next)
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e, independent.Next, noDelivery())
	next := e.Snapshot().Occurrences[1].ID
	calls := 0
	d.Submit = func(_ context.Context, w Wake) (string, error) {
		if w.ID != next {
			t.Fatalf("replayed original wake %s", w.ID)
		}
		calls++
		return "accepted", nil
	}
	tick(t, e, independent.Next, d)
	tick(t, e, independent.Next, d)
	if calls != 1 || !reflect.DeepEqual(e.Snapshot().Occurrences[0], originalReceipt) {
		t.Fatal("independent activation changed the original receipt")
	}
}

func TestParentSyncFailurePreservesAlreadyRenamedMutation(t *testing.T) {
	e, now := setup(t, nil)
	e.write = func(path string, value any) error {
		e.write = localstate.Write
		if err := localstate.Write(path, value); err != nil {
			return err
		}
		return &localstate.WriteError{Stage: "parent_sync", Err: os.ErrPermission}
	}
	p, err := saveTestPlan(e, Plan{ID: "renamed", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if localstate.DescribeFailure(err).Stage != "parent_sync" || len(e.Snapshot().Plans) != 0 {
		t.Fatalf("unconfirmed directory sync was acknowledged: %v", err)
	}
	next := Plan{ID: "second", Event: "mail", Kind: "after", Seconds: 2}
	if _, err := saveTestPlan(e, next, *now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(e.path)
	var state State
	if err != nil || json.Unmarshal(data, &state) != nil || len(state.Plans) != 2 || state.Plans[0].ID != p.ID || state.Plans[1].ID != next.ID {
		t.Fatalf("new write overtook an installed mutation: %+v, %v", state.Plans, err)
	}
}

func TestFailedRetirementCheckpointKeepsRevocationResult(t *testing.T) {
	e, now := setup(t, nil)
	e.state.Plans = []Plan{{ID: "legacy", Event: "legacy", GrantID: "care:mail", Created: *now, Next: *now, Status: "cancelled", Finished: *now}}
	if err := e.persist(); err != nil {
		t.Fatal(err)
	}
	d := noDelivery()
	var restore func()
	calls := 0
	d.Revoke = func(id string) error {
		if id != "care:mail" || calls != 0 {
			t.Fatalf("unexpected revoke %s, calls=%d", id, calls)
		}
		calls++
		restore = obstructLedger(t, e.path)
		return nil
	}
	requireRenameFailure(t, e.Tick(t.Context(), *now, d))
	restore()
	tick(t, e, now.Add(time.Minute), d)
	if calls != 1 || e.Snapshot().Plans[0].GrantID != "" {
		t.Fatal("revocation result was lost")
	}
}
