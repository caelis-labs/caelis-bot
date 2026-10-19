package eventplan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testSchema = json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`)

func setup(t *testing.T, run Runner) (*Engine, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	e, err := Open(filepath.Join(t.TempDir(), "events.json"), run, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	_, err = e.Register(Event{ID: "mail", Label: "Unread mail", Context: "Organize important mail", Kind: "tool", Server: "fixture/mail", Tool: "read", Binding: "v1", Arguments: map[string]any{}, OutputSchema: testSchema}, now)
	if err != nil {
		t.Fatal(err)
	}
	return e, &now
}
func noDelivery() Delivery {
	return Delivery{Receipt: func(string) string { return "unknown" }, Complete: func(string) bool { return false }}
}
func tick(t *testing.T, e *Engine, now time.Time, d Delivery) {
	t.Helper()
	if err := e.Tick(t.Context(), now, d); err != nil {
		t.Fatal(err)
	}
}
func collect(t *testing.T, e *Engine, now time.Time, d Delivery) {
	t.Helper()
	tick(t, e, now, d)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		ready := len(e.results) > 0
		e.mu.Unlock()
		if ready {
			tick(t, e, now, d)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("collector did not finish")
}
func TestCollectFilterWakeArchiveAndOriginalReceipt(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		if calls.Add(1) == 1 {
			return json.RawMessage(`{"count":2}`), nil
		}
		return json.RawMessage(`{"count":23}`), nil
	})
	p, err := e.Save(Plan{ID: "watch", Event: "mail", Kind: "interval", Seconds: 60, When: "event.count >= 20"}, *now)
	if err != nil {
		t.Fatal(err)
	}
	*now = p.Next
	collect(t, e, *now, noDelivery())
	if s := e.Snapshot(); s.Occurrences[0].Status != "filtered" {
		t.Fatal(s)
	}
	*now = now.Add(time.Minute)
	collect(t, e, *now, noDelivery())
	s := e.Snapshot()
	if s.Occurrences[1].Status != "pending" {
		t.Fatal(s)
	}
	wakeID := ""
	d := noDelivery()
	d.Ready = true
	d.Submit = func(_ context.Context, id, text, plan string) (string, error) {
		wakeID = id
		if plan != "watch" || !strings.Contains(text, "23") || !strings.Contains(text, "untrusted data") || !strings.Contains(text, "Organize important mail") {
			t.Fatal(text)
		}
		return "unknown", errors.New("transport lost")
	}
	tick(t, e, *now, d)
	// Restart must reconcile the original receipt, never replay collection or wake.
	restarted, err := Open(e.path, e.runner, *now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	d.Submit = func(context.Context, string, string, string) (string, error) {
		t.Fatal("replayed wake")
		return "", nil
	}
	d.Receipt = func(id string) string {
		if id != wakeID {
			t.Fatal(id)
		}
		return "accepted"
	}
	d.Complete = func(string) bool { return true }
	tick(t, restarted, *now, d)
	if calls.Load() != 2 {
		t.Fatal("replayed collector")
	}
	if err := restarted.Remove("watch", *now); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(25 * time.Hour)
	tick(t, restarted, *now, d)
	s = restarted.Snapshot()
	if s.Events[0].Archived.IsZero() || s.Plans[0].Finished.IsZero() {
		t.Fatal("active records retained", s)
	}
	*now = now.Add(8 * 24 * time.Hour)
	tick(t, restarted, *now, d)
	s = restarted.Snapshot()
	if len(s.Events)+len(s.Plans)+len(s.Occurrences) != 0 {
		t.Fatal("history not reclaimed", s)
	}
}
func TestAfterIdempotenceNoMatchAndImmutableDefinitions(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) { return json.RawMessage(`{"count":0}`), nil })
	original := Plan{ID: "once", Event: "mail", Kind: "after", Seconds: 10, When: "event.count > 0"}
	p, err := e.Save(original, *now)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5 * time.Second)
	again, err := e.Save(original, *now)
	if err != nil || !again.Next.Equal(p.Next) {
		t.Fatal("relative retry moved deadline", again, err)
	}
	changed := original
	changed.Seconds = 20
	if _, err = e.Save(changed, *now); err == nil {
		t.Fatal("rebound id")
	}
	v, _ := e.Event("mail")
	v.Created = time.Time{}
	v.LastUsed = time.Time{}
	if _, err = e.Register(v, *now); err != nil {
		t.Fatal("identical register", err)
	}
	v.Tool = "send"
	if _, err = e.Register(v, *now); err == nil {
		t.Fatal("changed event")
	}
	*now = p.Next
	collect(t, e, *now, noDelivery())
	s := e.Snapshot()
	if s.Plans[0].Status != "filtered" || s.Plans[0].Finished.IsZero() {
		t.Fatal(s)
	}
}
func TestUnknownCollectionIsBoundedAndNeverReplayed(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) { calls.Add(1); return nil, errors.New("unknown") })
	p, _ := e.Save(Plan{ID: "unknown", Event: "mail", Kind: "interval", Seconds: 60}, *now)
	*now = p.Next
	collect(t, e, *now, noDelivery())
	*now = now.Add(400 * 24 * time.Hour)
	tick(t, e, *now, noDelivery())
	if calls.Load() != 1 || e.Snapshot().Occurrences[0].Status != "unknown" {
		t.Fatal(e.Snapshot())
	}
	if err := e.Remove("unknown", *now); err != nil {
		t.Fatal(err)
	}
	if err := e.ArchiveEvent("mail", *now); err == nil {
		t.Fatal("archived uncertain collector")
	}
}
func TestExpirySchemaMismatchAndStorageFailure(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		return json.RawMessage(`{"count":"wrong"}`), nil
	})
	p, _ := e.Save(Plan{ID: "bad", Event: "mail", Kind: "after", Seconds: 1}, *now)
	*now = p.Next
	collect(t, e, *now, noDelivery())
	s := e.Snapshot()
	if s.Occurrences[0].Issue != "output_contract" || s.Occurrences[0].Status != "failed" {
		t.Fatal(s)
	}
	if _, err := e.Save(Plan{ID: "expired", Event: "mail", Kind: "after", Seconds: 1}, *now); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Hour)
	tick(t, e, *now, noDelivery())
	if e.Snapshot().Plans[1].Status != "expired" {
		t.Fatal(e.Snapshot())
	}
	e.write = func(string, any) error { return errors.New("disk unavailable") }
	if _, err := e.Save(Plan{ID: "no-write", Event: "mail", Kind: "after", Seconds: 1}, *now); err == nil {
		t.Fatal("write claimed")
	}
	if err := e.Tick(t.Context(), now.Add(time.Hour), noDelivery()); err == nil {
		t.Fatal("failed store executed")
	}
}
func TestSchemaSubsetRejectsAmbiguityAndRuntimeDrift(t *testing.T) {
	for _, raw := range []string{`{"type":"object","pattern":"["}`, `{"type":"object","required":[7]}`, `{"type":"object","properties":{"x":{"type":"array"}}}`, `{"type":"object","required":["absent"]}`} {
		if ValidateSchema(json.RawMessage(raw)) == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"count":1,"count":2}`, `{"count":1.5}`, `{}`, `{"count":"2"}`} {
		if _, err := ValidateOutput(testSchema, json.RawMessage(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	if _, err := ValidateOutput(testSchema, json.RawMessage(`{"count":2,"extra":"allowed"}`)); err != nil {
		t.Fatal(err)
	}
}
func TestAuthorizationBeforePersistenceAndCollectorStart(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		t.Fatal("executed during registration")
		return nil, nil
	})
	_, err := e.Save(Plan{ID: "denied", Event: "mail", Kind: "after", Seconds: 1}, *now, func() error { return errors.New("denied") })
	if err == nil || len(e.Snapshot().Plans) != 0 {
		t.Fatal("denied persisted")
	}
	raw, err := os.ReadFile(e.path)
	if err != nil || strings.Contains(string(raw), "denied") {
		t.Fatal("denied durable", err)
	}
}

func TestBusyBotDoesNotBlockOtherCollectors(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{"count":23}`), nil
	})
	p, err := e.Save(Plan{ID: "first", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Save(Plan{ID: "second", Event: "mail", Kind: "after", Seconds: 6}, *now); err != nil {
		t.Fatal(err)
	}
	*now = p.Next
	collect(t, e, *now, noDelivery())
	*now = now.Add(5 * time.Second)
	collect(t, e, *now, noDelivery())
	if calls.Load() != 2 || len(e.Snapshot().Occurrences) != 2 {
		t.Fatal("busy delivery blocked observation")
	}
	for _, o := range e.Snapshot().Occurrences {
		if o.Status != "pending" {
			t.Fatal(o)
		}
	}
}
func TestCompletionWaitsWithoutAcknowledgingAndOneShotFilters(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		if calls.Add(1) == 1 {
			return json.RawMessage(`{"status":"working"}`), nil
		}
		return json.RawMessage(`{"status":"completed"}`), nil
	})
	_, err := e.Register(Event{ID: "work", Kind: "task", Task: "owned-task", Label: "Wait for work", OutputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}},"required":["status"]}`)}, *now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Save(Plan{ID: "done", Event: "work", Kind: "completion", When: "event.status == 'failed'"}, *now); err != nil {
		t.Fatal(err)
	}
	collect(t, e, *now, noDelivery())
	if !e.Snapshot().Plans[0].Finished.IsZero() {
		t.Fatal("working task consumed callback")
	}
	*now = now.Add(30 * time.Second)
	collect(t, e, *now, noDelivery())
	if e.Snapshot().Plans[0].Status != "filtered" {
		t.Fatal(e.Snapshot())
	}
}
func TestWakeLimitsDoNotPreventCollectionAndRetirementWaitsForReceipts(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) { return json.RawMessage(`{"count":23}`), nil })
	p, _ := e.Save(Plan{ID: "wake", Event: "mail", Kind: "after", Seconds: 1}, *now)
	*now = p.Next
	collect(t, e, *now, noDelivery())
	submitted := 0
	d := noDelivery()
	d.Ready = true
	d.Submit = func(context.Context, string, string, string) (string, error) { submitted++; return "accepted", nil }
	d.Revoke = func(string) error { t.Fatal("revoked unresolved receipt"); return nil }
	tick(t, e, *now, d)
	if submitted != 1 {
		t.Fatal(submitted)
	}
	if err := e.Remove("wake", *now); err != nil {
		t.Fatal(err)
	}
	tick(t, e, *now, d)
	revoked := ""
	d.Revoke = func(id string) error { revoked = id; return nil }
	d.Complete = func(string) bool { return true }
	tick(t, e, *now, d)
	if revoked != "schedule:wake" {
		t.Fatal(revoked)
	}
}
func TestCloseCancelsAndJoinsCollector(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	e, now := setup(t, func(ctx context.Context, _ Event) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	p, _ := e.Save(Plan{ID: "slow", Event: "mail", Kind: "after", Seconds: 1}, *now)
	tick(t, e, p.Next, noDelivery())
	<-entered
	e.Close()
	select {
	case <-exited:
	default:
		t.Fatal("collector outlived close")
	}
}
func TestBoundedHistoryAndUnknownSurvivesPruning(t *testing.T) {
	e, now := setup(t, nil)
	e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: "unknown", Plan: "old", Event: "mail", Status: "unknown", At: now.Add(-30 * 24 * time.Hour)})
	for i := 0; i < MaxHistory+20; i++ {
		e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: "terminal", Status: "filtered", Finished: *now})
	}
	e.cleanup(*now)
	if len(e.state.Occurrences) != MaxHistory+1 || e.state.Occurrences[0].ID != "unknown" {
		t.Fatal("history bound lost uncertainty")
	}
}

func TestCorruptStorePreservedAndCannotExecute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	raw := []byte(`{"version":99}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, func(context.Context, Event) (json.RawMessage, error) {
		t.Fatal("executed corrupt state")
		return nil, nil
	}, time.Now()); err == nil {
		t.Fatal("corrupt store accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("corrupt evidence overwritten")
	}
}

func TestUnknownCollectorGetsOneMaintenanceWakeWithoutReplay(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		calls.Add(1)
		return nil, errors.New("transport failed")
	})
	p, _ := e.Save(Plan{ID: "maintain", Event: "mail", Kind: "interval", Seconds: 60}, *now)
	*now = p.Next
	collect(t, e, *now, noDelivery())
	s := e.Snapshot()
	if len(s.Occurrences) != 2 || s.Occurrences[0].Status != "unknown" || s.Occurrences[1].AttentionFor != s.Occurrences[0].ID {
		t.Fatal(s)
	}
	d := noDelivery()
	d.Ready = true
	wakes := 0
	d.Submit = func(_ context.Context, _ string, text, _ string) (string, error) {
		wakes++
		if !strings.Contains(text, "maintenance wakeup") || !strings.Contains(text, s.Occurrences[0].ID) {
			t.Fatal(text)
		}
		return "accepted", nil
	}
	d.Complete = func(string) bool { return true }
	tick(t, e, *now, d)
	*now = now.Add(time.Hour)
	tick(t, e, *now, d)
	if calls.Load() != 1 || wakes != 1 {
		t.Fatal("maintenance replayed collector", calls.Load(), wakes)
	}
}

func TestInterruptedCollectionRestoresOneMaintenanceWake(t *testing.T) {
	var calls atomic.Int32
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{"count":23}`), nil
	})
	_, err := e.Save(Plan{ID: "interrupted", Event: "mail", Kind: "after", Seconds: 1}, *now)
	if err != nil {
		t.Fatal(err)
	}
	e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: "occurrence:interrupted", Plan: "interrupted", Event: "mail", Status: "collecting", At: *now})
	if err = e.persist(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		restored, err := Open(e.path, e.runner, *now)
		if err != nil {
			t.Fatal(err)
		}
		tick(t, restored, *now, noDelivery())
		s := restored.Snapshot()
		if len(s.Occurrences) != 2 || s.Occurrences[0].Status != "unknown" || s.Plans[0].Status != "suspended" {
			t.Fatal(s)
		}
		restored.Close()
	}
	if calls.Load() != 0 {
		t.Fatal("interrupted collector replayed")
	}
}

func TestDeferredWakeKeepsOriginalOccurrence(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) { return json.RawMessage(`{"count":1}`), nil })
	p, _ := e.Save(Plan{ID: "once", Event: "mail", Kind: "after", Seconds: 1}, *now)
	collect(t, e, p.Next, noDelivery())
	id := e.Snapshot().Occurrences[0].ID
	d := noDelivery()
	d.Ready = true
	d.Submit = func(context.Context, string, string, string) (string, error) { return "deferred", nil }
	tick(t, e, p.Next, d)
	if s := e.Snapshot(); s.Occurrences[0].Status != "pending" || !s.Plans[0].Finished.IsZero() || !s.Plans[0].LastWake.IsZero() || len(s.WakeAttempts) != 0 {
		t.Fatal(s)
	}
	d.Submit = func(_ context.Context, got, _, _ string) (string, error) {
		if got != id {
			t.Fatal("identity changed")
		}
		return "accepted", nil
	}
	tick(t, e, p.Next.Add(time.Second), d)
	if s := e.Snapshot(); s.Occurrences[0].Status != "accepted" {
		t.Fatal(s)
	}
}
func TestCancelledCollectorCannotCreateMaintenanceWake(t *testing.T) {
	for _, kind := range []string{"confirmed", "unknown", "invalid", "valid"} {
		t.Run(kind, func(t *testing.T) {
			e, now := setup(t, nil)
			p, _ := e.Save(Plan{ID: "once", Event: "mail", Kind: "after", Seconds: 1}, *now)
			e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: "original", Plan: p.ID, Event: p.Event, At: *now, Status: "collecting"})
			if err := e.Remove(p.ID, *now); err != nil {
				t.Fatal(err)
			}
			done := completion{id: "original", output: json.RawMessage(`{"count":1}`)}
			switch kind {
			case "confirmed":
				done.err = collectorFailure("failed")
			case "unknown":
				done.err = errors.New("lost")
			case "invalid":
				done.output = json.RawMessage(`{}`)
			}
			e.finishCollection(done, *now)
			s := e.Snapshot()
			if len(s.Occurrences) != 1 || s.Plans[0].Status != "cancelled" {
				t.Fatal(s)
			}
			if kind == "unknown" && (s.Occurrences[0].Status != "unknown" || !s.Occurrences[0].Finished.IsZero()) {
				t.Fatal(s)
			}
		})
	}
}
func TestExplicitRejectionWithErrorIsNotUnknown(t *testing.T) {
	e, now := setup(t, func(context.Context, Event) (json.RawMessage, error) { return json.RawMessage(`{"count":1}`), nil })
	p, _ := e.Save(Plan{ID: "once", Event: "mail", Kind: "after", Seconds: 1}, *now)
	collect(t, e, p.Next, noDelivery())
	d := noDelivery()
	d.Ready = true
	d.Submit = func(context.Context, string, string, string) (string, error) {
		return "rejected", errors.New("revoked")
	}
	tick(t, e, p.Next, d)
	if s := e.Snapshot(); s.Occurrences[0].Status != "failed" {
		t.Fatal(s)
	}
}
func TestMaintenanceReferenceSurvivesHistoryPressure(t *testing.T) {
	e, now := setup(t, nil)
	e.state.Occurrences = []Occurrence{{ID: "original", Finished: *now, Status: "failed"}, {ID: "attention", AttentionFor: "original", At: *now, Status: "pending"}}
	for i := 0; i < MaxHistory+20; i++ {
		e.state.Occurrences = append(e.state.Occurrences, Occurrence{ID: fmt.Sprint(i), Finished: *now, Status: "filtered"})
	}
	e.cleanup(now.Add(time.Minute))
	if !slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.ID == "original" }) {
		t.Fatal("lost original evidence")
	}
}
func TestRetiredDefinitionsRotateBeyondCapacity(t *testing.T) {
	e, now := setup(t, nil)
	for i := 0; i < MaxActive+MaxHistory+20; i++ {
		id := fmt.Sprintf("plan-%d", i)
		if _, err := e.Save(Plan{ID: id, Event: "mail", Kind: "after", Seconds: 3600}, *now); err != nil {
			t.Fatal(i, err)
		}
		if err := e.Remove(id, *now); err != nil {
			t.Fatal(err)
		}
		e.retire(*now, nil)
	}
	for i := 0; i < MaxActive+MaxHistory+20; i++ {
		v := Event{ID: fmt.Sprintf("event-%d", i), Label: "Wake", Kind: "wake", OutputSchema: json.RawMessage(`{"type":"object"}`)}
		if _, err := e.Register(v, *now); err != nil {
			t.Fatal(i, err)
		}
		if err := e.ArchiveEvent(v.ID, *now); err != nil {
			t.Fatal(err)
		}
		e.retire(*now, nil)
	}
}
