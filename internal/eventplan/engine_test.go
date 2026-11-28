package eventplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, run Runner) (*Engine, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	e, err := Open(filepath.Join(t.TempDir(), "events.json"), run, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, &now
}

func noDelivery() Delivery { return Delivery{} }

func tick(t *testing.T, e *Engine, now time.Time, d Delivery) {
	t.Helper()
	if err := e.Tick(t.Context(), now, d); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, e *Engine, now time.Time, d Delivery) {
	t.Helper()
	tick(t, e, now, d)
	e.workers.Wait()
	tick(t, e, now, d)
}

func saveTestPlan(e *Engine, p Plan, now time.Time) (Plan, error) {
	v := Event{ID: p.ID, Label: p.ID, Kind: "wake"}
	if p.Event == "mail" && e.runner != nil {
		v.Kind, v.Command, p.When = "shell", "fixture", "true"
	}
	return e.Save(p, &v, now, nil)
}

func TestSharedObservationCollectsUntilAcceptedAndCanBeReused(t *testing.T) {
	for _, retain := range []bool{false, true} {
		t.Run(fmt.Sprintf("retain=%t", retain), func(t *testing.T) {
			samples := 0
			e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
				samples++
				return json.RawMessage(fmt.Sprintf(`{"version":%d}`, samples)), nil
			})
			def := Event{ID: "index", Label: "New index", Command: "read-index", Kind: "shell", Retain: retain}
			request := Plan{ID: "original", Kind: "interval", Seconds: 60, When: "event.version >= 2"}
			original, err := e.Save(request, &def, *now, nil)
			if err != nil {
				t.Fatal(err)
			}
			shared, err := e.Save(Plan{ID: "shared", Event: def.ID, Kind: "after", Seconds: 121}, nil, *now, nil)
			if err != nil {
				t.Fatal(err)
			}
			var wakes []Wake
			d := Delivery{Ready: func() bool { return true }, Submit: func(_ context.Context, w Wake) (string, error) {
				wakes = append(wakes, w)
				return "accepted", nil
			}}
			collect(t, e, original.Next, d)
			if len(wakes) != 0 {
				t.Fatal("nonmatch woke")
			}
			collect(t, e, original.Next.Add(time.Minute), d)
			s := e.Snapshot()
			if len(wakes) != 1 || !strings.Contains(wakes[0].Text, `{"version":2}`) || len(s.Events) != 1 ||
				s.Plans[0].Finished.IsZero() || !s.Plans[1].Finished.IsZero() {
				t.Fatal("acceptance deleted a shared definition or did not consume the original", s)
			}
			if err := e.RemoveEvent(def.ID); err == nil {
				t.Fatal("removed an event with a consumer")
			}
			collect(t, e, shared.Next, d)
			s = e.Snapshot()
			if len(wakes) != 2 || len(s.Events) != map[bool]int{true: 1, false: 0}[retain] {
				t.Fatal("wrong definition retention after last reference", s)
			}
			again, err := e.Save(request, &def, shared.Next, func() bool { t.Fatal("duplicate probed capability"); return false })
			if err != nil || again.Finished.IsZero() {
				t.Fatal("duplicate registration did not return original settlement", again, err)
			}
			if retain {
				next, err := e.Save(Plan{ID: "reuse", Event: def.ID, Kind: "after", Seconds: 1}, nil, shared.Next, nil)
				if err != nil {
					t.Fatal(err)
				}
				collect(t, e, next.Next, d)
				if len(wakes) != 3 || wakes[2].ID == wakes[0].ID {
					t.Fatal("reuse did not create an independent occurrence", wakes)
				}
				if err := e.RemoveEvent(def.ID); err != nil {
					t.Fatal(err)
				}
			}
			tick(t, e, shared.Next.Add(8*24*time.Hour), d)
			s = e.Snapshot()
			if len(s.Events)+len(s.Plans)+len(s.Occurrences) != 0 {
				t.Fatal("storage did not return to idle", s)
			}
			t.Logf("%d samples, %d wakes, one shot per arrangement, no definition after final cleanup", samples, len(wakes))
		})
	}
}

func TestFormerSharedBudgetDoesNotBlockIndependentReminders(t *testing.T) {
	e, now := setup(t, nil)
	for i := range 2 {
		id := fmt.Sprintf("clock-%d", i)
		if _, err := saveTestPlan(e, Plan{ID: id, Kind: "after", Seconds: 10 + i}, *now); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(e.path)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	_ = json.Unmarshal(raw, &legacy)
	legacy["version"] = 1
	legacy["policy"] = map[string]int{"maximumInterruptionsPer24Hours": 1, "minimumGapSeconds": 300}
	legacy["wakeAttempts"] = []time.Time{*now}
	legacy["wakeReservations"] = []time.Time{*now}
	raw, _ = json.Marshal(legacy)
	if err = os.WriteFile(e.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var ids []string
	d := Delivery{Ready: func() bool { return true }, Submit: func(_ context.Context, w Wake) (string, error) {
		ids = append(ids, w.ID)
		return "accepted", nil
	}}
	tick(t, restored, now.Add(9*time.Second), d)
	if len(ids) != 0 {
		t.Fatal("early reminder")
	}
	for _, seconds := range []int{10, 11, 12} {
		tick(t, restored, now.Add(time.Duration(seconds)*time.Second), d)
	}
	if len(ids) != 2 || ids[0] == ids[1] || len(restored.Snapshot().Events) != 0 {
		t.Fatal("legacy quota or gap still affected due clocks", ids)
	}
	upgraded, _ := os.ReadFile(e.path)
	if strings.Contains(string(upgraded), "wakeAttempts") || strings.Contains(string(upgraded), "minimumGapSeconds") {
		t.Fatal("retired policy was persisted")
	}
}

func TestUnconfirmedWakeRestartsWithOriginalIdentity(t *testing.T) {
	e, now := setup(t, nil)
	plan, err := saveTestPlan(e, Plan{ID: "original", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var original Wake
	d := Delivery{Ready: func() bool { return true }, Submit: func(_ context.Context, w Wake) (string, error) {
		calls++
		original = w
		return "unknown", nil
	}}
	tick(t, e, plan.Next, d)
	restored, err := Open(e.path, nil, plan.Next)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	ready := false
	d.Receipt = func(id string) string {
		if id != original.ID {
			t.Fatal("queried a replacement ID", id)
		}
		if ready {
			return "accepted"
		}
		return "unknown"
	}
	tick(t, restored, plan.Next.Add(time.Hour), d)
	ready = true
	tick(t, restored, plan.Next.Add(time.Hour), d)
	tick(t, restored, plan.Next.Add(time.Hour), d)
	s := restored.Snapshot()
	if calls != 1 || len(s.Events) != 0 || s.Occurrences[0].ID != original.ID || s.Occurrences[0].Status != "completed" {
		t.Fatal("unconfirmed wake was replayed or lost", s)
	}
}

func TestBusyDeferralAndRejectedAttemptRetainLifecycle(t *testing.T) {
	e, now := setup(t, nil)
	plan, err := saveTestPlan(e, Plan{ID: "busy", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	var attempts []string
	d := Delivery{Ready: func() bool { return true }, Submit: func(_ context.Context, w Wake) (string, error) {
		attempts = append(attempts, w.ID)
		return []string{"deferred", "rejected", "accepted"}[len(attempts)-1], nil
	}}
	tick(t, e, plan.Next, noDelivery())
	late := plan.Next.Add(2 * time.Hour)
	tick(t, e, late, d)
	tick(t, e, late, d)
	for range 3 {
		tick(t, e, late, d)
	}
	// A definitive rejection after expiry cannot become an unbounded retry.
	if len(attempts) != 2 || attempts[0] != attempts[1] {
		t.Fatal("busy attempt replaced the original occurrence or churned", attempts)
	}
}

func TestOneShotStorageFallsBackToBoundedDedup(t *testing.T) {
	for _, persist := range []bool{false, true} {
		t.Run(fmt.Sprintf("persist=%t", persist), func(t *testing.T) {
			e, now := setup(t, nil)
			total := MaxHistory * 5
			if persist {
				total = 12
			} else {
				e.write = func(string, any) error { return nil }
			}
			wakes := 0
			d := Delivery{Ready: func() bool { return true }, Submit: func(context.Context, Wake) (string, error) { wakes++; return "accepted", nil }}
			for i := range total {
				id := fmt.Sprintf("clock-%d", i)
				p, err := saveTestPlan(e, Plan{ID: id, Kind: "after", Seconds: 1}, *now)
				if err != nil {
					t.Fatal(err)
				}
				tick(t, e, p.Next, d)
				*now = p.Next
			}
			s := e.Snapshot()
			if wakes != total || len(s.Events) != 0 || len(s.Occurrences) > MaxHistory || len(s.Plans) > MaxHistory*2 {
				t.Fatal("settled arrangements accumulated", wakes, len(s.Events), len(s.Occurrences), len(s.Plans))
			}
			after := now.Add(Retention + time.Second)
			tick(t, e, after, d)
			if s = e.Snapshot(); len(s.Events)+len(s.Plans)+len(s.Occurrences) != 0 {
				t.Fatal("dedup retention did not expire", s)
			}
			if persist {
				raw, err := os.ReadFile(e.path)
				if err != nil || len(raw) > 100 {
					t.Fatal("settled state remained on disk", len(raw), err)
				}
			}
			if !e.NextCheck(after).IsZero() {
				t.Fatal("idle scheduler kept a timer")
			}
			t.Logf("%d one-shot wakes, 0 active, 0 retained after expiry", total)
		})
	}
}

func TestInterruptedCollectionNeverReplaysOrWakes(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		close(started)
		<-release
		return nil, errors.New("unknown output")
	})
	defer close(release)
	p, err := saveTestPlan(e, Plan{ID: "observation", Event: "mail", Kind: "interval", Seconds: 60}, *now)
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e, p.Next, noDelivery())
	<-started
	restored, err := Open(e.path, func(context.Context, Event) (json.RawMessage, error) {
		t.Error("interrupted sample replayed")
		return nil, nil
	}, p.Next)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	d := Delivery{Ready: func() bool { return true }, Submit: func(context.Context, Wake) (string, error) {
		t.Error("unconfirmed sample dispatched")
		return "accepted", nil
	}}
	tick(t, restored, p.Next.Add(time.Hour), d)
	s := restored.Snapshot()
	if len(s.Occurrences) != 1 || s.Occurrences[0].Status != "unknown" || s.Plans[0].Status != "suspended" {
		t.Fatal("interrupted identity not preserved", s)
	}
}
