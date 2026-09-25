package care

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

var instant = time.Date(2026, 9, 25, 4, 0, 0, 0, time.UTC)

func rule(id string) Rule {
	return Rule{ID: id, Label: id, On: "desktop.usage", When: "event.activeSeconds >= 7200 && event.idleSeconds < 120", Prompt: "Offer a short break", TimeZone: "Asia/Shanghai", CooldownSeconds: 60, ExpiresSeconds: 3600}
}
func opened(t *testing.T) *Engine {
	t.Helper()
	e, err := Open(filepath.Join(t.TempDir(), "care.json"))
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func save(t *testing.T, e *Engine, r Rule) Registration {
	t.Helper()
	v, err := e.Save(t.Context(), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func receive(t *testing.T, e *Engine, now time.Time) {
	t.Helper()
	if err := e.Receive(t.Context(), Event{Source: "desktop.usage", At: now, Data: map[string]any{"activeSeconds": 7300, "idleSeconds": 30}}, now); err != nil {
		t.Fatal(err)
	}
}
func presence() Presence              { yes := true; return Presence{true, &yes} }
func noReceipt(id string) api.Receipt { return api.Receipt{ID: id, Outcome: "unknown"} }
func accepted(_ context.Context, a Activation) (api.Receipt, error) {
	return api.Receipt{ID: a.ID, Outcome: "accepted"}, nil
}
func deliver(t *testing.T, e *Engine, now time.Time) {
	t.Helper()
	if err := e.Deliver(t.Context(), now, presence(), true, noReceipt, accepted); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledOperationsHaveNoEffects(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	receive(t, e, instant)
	before := e.Snapshot()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.Save(ctx, rule("new"), func(context.Context, string, string) error {
		t.Fatal("cancelled grant called")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := e.Remove(ctx, "x", func(context.Context, string) error {
		t.Fatal("cancelled revoke called")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := e.Receive(ctx, Event{Source: "clock.minute", At: instant, Data: map[string]any{}}, instant); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := e.Deliver(ctx, instant, presence(), true, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
		t.Fatal("cancelled dispatch called")
		return api.Receipt{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("cancelled operation changed durable state")
	}
}

func TestGrantNamespaceAndEventNumberBounds(t *testing.T) {
	if identifier.MatchString(GrantID("break")) {
		t.Fatal("care grant can alias a reminder ID")
	}
	for _, value := range []any{json.Number("1e999"), []any{json.Number("1e999")}, map[string]any{"number": json.Number("1e999")}} {
		if _, err := eventData(Event{Data: map[string]any{"nested": value}}); err == nil {
			t.Fatal("overflow accepted")
		}
	}
}

func TestDefinitiveReceiptSurvivesExplanatoryError(t *testing.T) {
	for _, outcome := range []string{"rejected", "accepted"} {
		t.Run(outcome, func(t *testing.T) {
			e := opened(t)
			save(t, e, rule("x"))
			receive(t, e, instant)
			err := e.Deliver(t.Context(), instant, presence(), true, noReceipt, func(_ context.Context, a Activation) (api.Receipt, error) {
				return api.Receipt{ID: a.ID, Outcome: outcome}, errors.New("native explanation")
			})
			if err == nil || e.Snapshot().Activations[0].Status != outcome || e.Status() != "" {
				t.Fatal("exact receipt treated as unknown", err, e.Snapshot())
			}
		})
	}
}
func TestConditionsAndLocalTime(t *testing.T) {
	for _, tc := range []struct {
		name, expr string
		data       map[string]any
		at         time.Time
		match      bool
	}{
		{"usage", "event.activeSeconds >= 7200 && event.idleSeconds < 120", map[string]any{"activeSeconds": 7200.0, "idleSeconds": 30.0}, instant, true},
		{"false", "event.activeSeconds >= 7200", map[string]any{"activeSeconds": 7199}, instant, false},
		{"localdate", "local.month == 9 && local.day == 25 && local.hour == 12 && local.weekday == 5", map[string]any{}, instant, true},
		{"app", "event.application in ['com.apple.Terminal', 'com.microsoft.VSCode']", map[string]any{"application": "com.apple.Terminal"}, instant, true},
		{"collections", "event.items.exists(x, x.score >= 80 && x.tags.exists(t, t == 'important'))", map[string]any{"items": []any{map[string]any{"score": 90, "tags": []string{"important"}}}}, instant, true},
		{"timestamp", "now >= timestamp('2026-09-25T04:00:00Z')", map[string]any{}, instant, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := rule("x")
			r.When = tc.expr
			got, err := Test(t.Context(), r, tc.data, tc.at)
			if err != nil || got != tc.match {
				t.Fatal(got, err)
			}
		})
	}
	r := rule("dst")
	r.TimeZone = "America/New_York"
	r.When = "local.hour == 1 && local.minute == 30"
	for _, raw := range []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"} {
		at, _ := time.Parse(time.RFC3339, raw)
		if got, err := Test(t.Context(), r, map[string]any{}, at); err != nil || !got {
			t.Fatal(got, err)
		}
	}
}
func TestValidationRejectsUnsupportedAndExpensivePrograms(t *testing.T) {
	for name, mutate := range map[string]func(*Rule){
		"unknownSource": func(r *Rule) { r.On = "mail.changed" }, "invalidID": func(r *Rule) { r.ID = "../x" }, "zone": func(r *Rule) { r.TimeZone = "" }, "localzone": func(r *Rule) { r.TimeZone = "Local" }, "prompt": func(r *Rule) { r.Prompt = strings.Repeat("x", 4097) }, "label": func(r *Rule) { r.Label = " " }, "cooldown": func(r *Rule) { r.CooldownSeconds = 59 }, "expiry": func(r *Rule) { r.ExpiresSeconds = 86401 }, "syntax": func(r *Rule) { r.When = "event.x = 2" }, "notbool": func(r *Rule) { r.When = "42" }, "io": func(r *Rule) { r.When = "exec('ls') == true" }, "oversize": func(r *Rule) { r.When = strings.Repeat("!", 2049) + "true" }, "recursion": func(r *Rule) { r.When = strings.Repeat("(", 80) + "true" + strings.Repeat(")", 80) },
	} {
		t.Run(name, func(t *testing.T) {
			e := opened(t)
			r := rule("x")
			mutate(&r)
			called := false
			_, err := e.Save(t.Context(), r, func(context.Context, string, string) error { called = true; return nil })
			if err == nil || called || len(e.Snapshot().Rules) != 0 {
				t.Fatal("invalid rule reached authorization", err)
			}
		})
	}
	r := rule("cost")
	r.When = "event.values.all(a, event.values.all(b, a == b))"
	vals := make([]int, 500)
	if _, err := Test(t.Context(), r, map[string]any{"values": vals}, instant); err == nil {
		t.Fatal("cost bound bypassed")
	}
	r.When = "true"
	if _, err := Test(t.Context(), r, map[string]any{"body": strings.Repeat("x", MaxEventBytes)}, instant); err == nil {
		t.Fatal("size bound bypassed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Test(ctx, r, map[string]any{}, instant); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	r.When = "event.absent > 0"
	if _, err := Test(t.Context(), r, map[string]any{}, instant); err == nil {
		t.Fatal("missing field accepted")
	}
}
func TestDedupCooldownCoalescingAndPresence(t *testing.T) {
	e := opened(t)
	save(t, e, rule("break"))
	receive(t, e, instant)
	receive(t, e, instant)
	receive(t, e, instant.Add(time.Minute))
	if len(e.Snapshot().Activations) != 1 {
		t.Fatal("did not coalesce")
	}
	locked := false
	for _, p := range []Presence{{true, nil}, {true, &locked}, {false, presence().Unlocked}} {
		if err := e.Deliver(t.Context(), instant, p, true, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
			t.Fatal("presence bypass")
			return api.Receipt{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = e.Deliver(t.Context(), instant, presence(), false, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
		t.Fatal("busy steer")
		return api.Receipt{}, nil
	})
	deliver(t, e, instant)
	reloaded, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	receive(t, reloaded, instant) // Same source high-water mark survives restart.
	_ = reloaded.Deliver(t.Context(), instant, presence(), true, noReceipt, func(context.Context, Activation) (api.Receipt, error) { t.Fatal("replay"); return api.Receipt{}, nil })
	receive(t, reloaded, instant.Add(2*time.Minute))
	deliver(t, reloaded, instant.Add(2*time.Minute))
	if len(reloaded.Snapshot().Attempts) != 1 {
		t.Fatal("global gap bypassed")
	}
	deliver(t, reloaded, instant.Add(5*time.Minute))
	if len(reloaded.Snapshot().Attempts) != 2 {
		t.Fatal("pending never released")
	}
}
func TestReplacementRemovalAndAuthorizationFailure(t *testing.T) {
	e := opened(t)
	old := save(t, e, rule("x"))
	receive(t, e, instant)
	unchanged := save(t, e, rule("x"))
	if unchanged.Version != old.Version || len(e.Snapshot().Activations) != 1 {
		t.Fatal("identical save changed occurrence")
	}
	r := rule("x")
	r.Prompt = "Changed task"
	candidate, err := e.Save(t.Context(), r, func(context.Context, string, string) error { return errors.New("lost grant reply") })
	if err == nil || candidate.Enabled || candidate.Version == old.Version || e.Snapshot().Activations[0].Status != "cancelled" {
		t.Fatal(candidate, err)
	}
	retry, err := e.Save(t.Context(), r, func(_ context.Context, id, fp string) error {
		if id != "care:x" || !strings.Contains(fp, candidate.Version) {
			t.Fatal(id, fp)
		}
		return nil
	})
	if err != nil || retry.Version != candidate.Version || !retry.Enabled {
		t.Fatal(retry, err)
	}
	receive(t, e, instant.Add(2*time.Minute))
	if err = e.Remove(t.Context(), "x", func(context.Context, string) error { return errors.New("revocation failed") }); err == nil {
		t.Fatal("missing error")
	}
	if e.Snapshot().Rules[0].Enabled {
		t.Fatal("failed revocation revived rule")
	}
	if err = e.Remove(t.Context(), "x", nil); err != nil {
		t.Fatal(err)
	}
	recreated := save(t, e, r)
	if recreated.Version == candidate.Version {
		t.Fatal("recreated old grant identity")
	}
	deliver(t, e, instant.Add(3*time.Minute))
	if len(e.Snapshot().Attempts) != 0 {
		t.Fatal("removed work dispatched")
	}
}
func TestUnknownMismatchedAndRejectedReceipts(t *testing.T) {
	for _, outcome := range []string{"unknown", "rejected", "wrong-id", "error"} {
		t.Run(outcome, func(t *testing.T) {
			e := opened(t)
			save(t, e, rule("x"))
			receive(t, e, instant)
			err := e.Deliver(t.Context(), instant, presence(), true, noReceipt, func(_ context.Context, a Activation) (api.Receipt, error) {
				r := api.Receipt{ID: a.ID, Outcome: outcome}
				if outcome == "wrong-id" {
					r = api.Receipt{ID: "other", Outcome: "accepted"}
				}
				if outcome == "error" {
					return r, errors.New("lost reply")
				}
				return r, nil
			})
			if outcome == "error" && err == nil {
				t.Fatal("lost error")
			}
			e, err = Open(e.path)
			if err != nil {
				t.Fatal(err)
			}
			status := e.Snapshot().Activations[0].Status
			if outcome == "rejected" {
				if status != "rejected" {
					t.Fatal(status)
				}
				return
			}
			if status != "unknown" {
				t.Fatal(status)
			}
			_ = e.Deliver(t.Context(), instant.Add(time.Hour), presence(), true, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
				t.Fatal("unknown retried")
				return api.Receipt{}, nil
			})
			_ = e.Deliver(t.Context(), instant.Add(time.Hour), Presence{}, false, func(id string) api.Receipt { return api.Receipt{ID: id, Outcome: "accepted"} }, accepted)
			if e.Snapshot().Activations[0].Status != "accepted" {
				t.Fatal("did not reconcile while locked")
			}
		})
	}
}
func TestExpiryMalformedEventsAndRuleErrorIsolation(t *testing.T) {
	e := opened(t)
	save(t, e, rule("valid"))
	bad := rule("bad")
	bad.When = "event.missing == 1"
	save(t, e, bad)
	receive(t, e, instant)
	if e.Snapshot().Rules[1].Issue != "condition_error" || len(e.Snapshot().Activations) != 1 {
		t.Fatal("error poisoned other rule")
	}
	for _, ev := range []Event{{Source: "custom", At: instant, Data: map[string]any{}}, {Source: "desktop.usage", At: instant.Add(-6 * time.Minute), Data: map[string]any{}}, {Source: "desktop.usage", At: instant.Add(time.Minute), Data: map[string]any{}}} {
		if err := e.Receive(t.Context(), ev, instant); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
	_ = e.Deliver(t.Context(), instant.Add(time.Hour), Presence{}, false, noReceipt, accepted)
	if e.Snapshot().Activations[0].Status != "expired" {
		t.Fatal("locked queue did not expire")
	}
}
func TestBudgetsRetentionAndCorruptState(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	for i := 0; i < 9; i++ {
		now := instant.Add(time.Duration(i) * 10 * time.Minute)
		receive(t, e, now)
		deliver(t, e, now)
	}
	if len(e.Snapshot().Attempts) != 8 || e.Snapshot().Activations[8].Status != "pending" {
		t.Fatal("daily budget bypass")
	}
	// Long-lived installation never exhausts idempotency records: terminal
	// receipts are bounded, source watermarks continue rejecting old events.
	e.write = func(string, any) error { return nil }
	for i := 1; i < 400; i++ {
		now := instant.Add(time.Duration(i) * 25 * time.Hour)
		receive(t, e, now)
		deliver(t, e, now)
	}
	if len(e.Snapshot().Activations) > 256 {
		t.Fatal("unbounded receipts")
	}
	count := len(e.Snapshot().Activations)
	receive(t, e, instant)
	if len(e.Snapshot().Activations) != count {
		t.Fatal("old event replay")
	}
	if err := os.WriteFile(e.path, []byte(`{"version":999}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(e.path); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
func TestPersistenceFailureBeforeAndAfterDispatch(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			e := opened(t)
			save(t, e, rule("x"))
			receive(t, e, instant)
			count := 0
			e.write = func(string, any) error { return errors.New("disk full") }
			if after {
				e.write = durableWrite
			}
			err := e.Deliver(t.Context(), instant, presence(), true, noReceipt, func(_ context.Context, a Activation) (api.Receipt, error) {
				count++
				e.write = func(string, any) error { return errors.New("disk full") }
				return api.Receipt{ID: a.ID, Outcome: "accepted"}, nil
			})
			if err == nil || count != map[bool]int{false: 0, true: 1}[after] {
				t.Fatal(count, err)
			}
			_ = e.Deliver(t.Context(), instant.Add(time.Hour), presence(), true, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
				t.Fatal("write failure retried")
				return api.Receipt{}, nil
			})
			if after {
				e2, err := Open(e.path)
				if err != nil || e2.Snapshot().Activations[0].Status != "unknown" {
					t.Fatal("crash window lost", err)
				}
			}
		})
	}
}
func TestConcurrentReceiveAndRemovalAreSerialized(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			_ = e.Receive(t.Context(), Event{Source: "desktop.usage", At: instant, Data: map[string]any{"activeSeconds": 7300, "idleSeconds": 0}}, instant)
			_ = e.Snapshot()
		})
	}
	wg.Wait()
	if len(e.Snapshot().Activations) != 1 {
		t.Fatal("duplicate concurrent admission")
	}
	if err := e.Remove(t.Context(), "x", nil); err != nil {
		t.Fatal(err)
	}
	deliver(t, e, instant)
	if len(e.Snapshot().Attempts) != 0 {
		t.Fatal("removed rule dispatched")
	}
}
