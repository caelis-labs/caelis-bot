package eventplan

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestPendingClockExpiresAtExplicitDeadline(t *testing.T) {
	for _, version := range []string{"current", "upgrade-v1"} {
		t.Run(version, func(t *testing.T) {
			e, start := setup(t, nil)
			deadline := start.Add(2 * time.Minute)
			p, err := saveTestPlan(e, Plan{ID: "clock", Kind: "after", Seconds: 60, Until: deadline.Format(time.RFC3339)}, *start)
			if err != nil {
				t.Fatal(err)
			}
			tick(t, e, p.Next, noDelivery())
			original := e.Snapshot().Occurrences[0]
			if original.Status != "pending" {
				t.Fatal(original)
			}
			if version == "upgrade-v1" {
				legacy := e.Snapshot()
				legacy.Version = 1
				raw, _ := json.Marshal(legacy)
				e.Close()
				if err := os.WriteFile(e.path, raw, 0600); err != nil {
					t.Fatal(err)
				}
				e, err = Open(e.path, nil, p.Next)
				if err != nil {
					t.Fatal(err)
				}
				defer e.Close()
			}
			d := Delivery{Ready: func() bool { return true }, Submit: func(context.Context, Wake) (string, error) {
				t.Error("expired wake was submitted")
				return "accepted", nil
			}}
			tick(t, e, deadline, d)
			tick(t, e, deadline.Add(time.Second), d)
			s := e.Snapshot()
			if s.Version != 2 || len(s.Occurrences) != 1 || s.Occurrences[0].ID != original.ID ||
				s.Occurrences[0].Status != "expired" || s.Plans[0].Status != "expired" || len(s.Events) != 0 {
				t.Fatalf("deadline did not settle pending work: %+v", s)
			}
		})
	}
}

func TestClockDeadlineRecheckedAfterPersistingDispatchIntent(t *testing.T) {
	e, start := setup(t, nil)
	deadline := start.Add(2 * time.Minute)
	p, err := saveTestPlan(e, Plan{ID: "clock", Kind: "after", Seconds: 60, Until: deadline.Format(time.RFC3339)}, *start)
	if err != nil {
		t.Fatal(err)
	}
	now := p.Next
	write := e.write
	e.write = func(path string, value any) error {
		if err := write(path, value); err != nil {
			return err
		}
		s := value.(State)
		if len(s.Occurrences) == 1 && s.Occurrences[0].Status == "dispatching" {
			now = deadline
		}
		return nil
	}
	d := Delivery{Now: func() time.Time { return now }, Ready: func() bool { return true }, Submit: func(context.Context, Wake) (string, error) {
		t.Error("submitted after the persisted intent crossed the deadline")
		return "accepted", nil
	}}
	tick(t, e, p.Next, d)
	e.Close()
	reopened, err := Open(e.path, nil, deadline)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s := reopened.Snapshot()
	if s.Occurrences[0].Status != "expired" || s.Plans[0].Status != "expired" {
		t.Fatal("undispatched intent was not settled durably", s)
	}
}

func TestLateWakeReceiptDeterminesEligibleCleanupAfterCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name := "expired"
		if cancel {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			e, start := setup(t, nil)
			deadline := start.Add(2 * time.Minute)
			p, err := saveTestPlan(e, Plan{ID: "clock", Kind: "after", Seconds: 60, Until: deadline.Format(time.RFC3339)}, *start)
			if err != nil {
				t.Fatal(err)
			}
			var submissions, lookups []string
			d := Delivery{Ready: func() bool { return true }, Submit: func(_ context.Context, w Wake) (string, error) {
				submissions = append(submissions, w.ID)
				return "unknown", nil
			}}
			tick(t, e, p.Next, d)
			if cancel {
				if err := e.Remove(p.ID, p.Next.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			e.Close()
			resolvedAt := deadline.Add(Retention + time.Hour)
			reopened, err := Open(e.path, nil, resolvedAt)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if next := reopened.NextCheck(resolvedAt); !next.IsZero() {
				t.Fatal("unresolved receipt should wait for a native edge", next)
			}
			d.Receipt = func(id string) string {
				lookups = append(lookups, id)
				return "accepted"
			}
			tick(t, reopened, resolvedAt, d)
			s := reopened.Snapshot()
			wantStatus := "completed"
			if cancel {
				wantStatus = "cancelled"
			}
			if len(submissions) != 1 || !reflect.DeepEqual(submissions, lookups) ||
				s.Plans[0].Status != wantStatus || s.Occurrences[0].Status != "completed" {
				t.Fatal("lost original receipt or replayed", submissions, lookups, s)
			}
			checkAt := resolvedAt.Add(Retention)
			if next := reopened.NextCheck(resolvedAt); !next.Equal(checkAt) {
				t.Fatalf("next check = %v, want %v", next, checkAt)
			}
			tick(t, reopened, checkAt, d)
			if s = reopened.Snapshot(); len(s.Plans)+len(s.Occurrences) != 0 || !reopened.NextCheck(checkAt).IsZero() {
				t.Fatal("eligible retention did not clear the ledger and timer", s)
			}
		})
	}
}

func TestSaveRevalidatesAfterConcurrentChangeDuringShellProbe(t *testing.T) {
	for _, mode := range []string{"replaced-definition", "existing-retry", "conflicting-id"} {
		t.Run(mode, func(t *testing.T) {
			e, now := setup(t, nil)
			def := Event{ID: "shared", Kind: "shell", Label: "Workspace state", Command: "first", Retain: true}
			p := Plan{ID: "observe", Kind: "interval", Seconds: 60, When: "true"}
			var inline *Event
			if mode == "replaced-definition" {
				owner := Plan{ID: "owner", Kind: "interval", Seconds: 60, When: "true"}
				if _, err := e.Save(owner, &def, *now, nil); err != nil {
					t.Fatal(err)
				}
				if err := e.Remove(owner.ID, *now); err != nil {
					t.Fatal(err)
				}
				p.Event = def.ID
			} else {
				inline = &def
			}
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			saved := make(chan error, 1)
			go func() {
				_, err := e.Save(p, inline, *now, func() bool {
					close(entered)
					<-release
					return mode != "existing-retry"
				})
				saved <- err
			}()
			<-entered
			change := make(chan error, 1)
			go func() {
				var err error
				switch mode {
				case "replaced-definition":
					if err = e.RemoveEvent(def.ID); err == nil {
						replacement := def
						replacement.Command = "replacement"
						_, err = e.Save(Plan{ID: "replacement", Kind: "interval", Seconds: 60, When: "true"}, &replacement, *now, nil)
					}
				case "existing-retry":
					_, err = e.Save(p, inline, *now, nil)
				case "conflicting-id":
					_, err = saveTestPlan(e, Plan{ID: p.ID, Kind: "after", Seconds: 1}, *now)
				}
				change <- err
			}()
			select {
			case err := <-change:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("ledger mutation blocked on capability probe")
			}
			release <- struct{}{}
			if err := <-saved; (err == nil) != (mode == "existing-retry") {
				t.Fatal("probe committed against the wrong ledger state", err)
			}
			s := e.Snapshot()
			if mode == "replaced-definition" {
				if len(s.Events) != 1 || s.Events[0].Command != "replacement" || len(s.Plans) != 2 {
					t.Fatal("changed definition was bound to pending registration", s)
				}
			} else if len(s.Plans) != 1 || len(s.Events) != 1 {
				t.Fatal("competing request created more than one schedule", s)
			}
		})
	}
}

func TestPendingCancellationOnlyStopsSamplingWhenWriteCommits(t *testing.T) {
	for _, success := range []bool{false, true} {
		name := "rollback"
		if success {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			running := make(chan context.Context, 1)
			finish := make(chan struct{})
			e, now := setup(t, func(ctx context.Context, _ Event) (json.RawMessage, error) {
				running <- ctx
				select {
				case <-ctx.Done():
				case <-finish:
				}
				return json.RawMessage(`{}`), nil
			})
			defer close(finish)
			p, err := saveTestPlan(e, Plan{ID: "sample", Event: "mail", Kind: "after", Seconds: 1}, *now)
			if err != nil {
				t.Fatal(err)
			}
			tick(t, e, p.Next, noDelivery())
			sample := <-running
			release := delayLedgerWrite(e)
			if err := e.Remove(p.ID, p.Next); err == nil {
				t.Fatal("expected a deferred write")
			}
			if sample.Err() != nil {
				t.Fatal("cancellation before the write committed")
			}
			if success {
				settleDelayedLedger(t, e, release, "")
				if sample.Err() != context.Canceled {
					t.Fatal("completed removal did not cancel the sample", sample.Err())
				}
			} else {
				restore := obstructLedger(t, e.path)
				settleDelayedLedger(t, e, release, "rename")
				restore()
				if sample.Err() != nil {
					t.Fatal("failed removal cancelled the live sample")
				}
			}
		})
	}
}
