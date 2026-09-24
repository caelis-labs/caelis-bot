package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgrammableRules(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	rules := []Rule{
		{ID: "usage", On: "desktop.usage", When: `event.seconds >= 7200 && event.app in ['editor','terminal']`, Prompt: "Offer a break", CooldownSeconds: 3600},
		{ID: "birthday", On: "calendar.date", When: `event.month == 9 && event.day == 24`, Prompt: "Prepare a greeting"},
		{ID: "inbox", On: "mail.changed", When: `event.labels.exists(x, x == 'inbox') && !event.sender.endsWith('@noise.example')`, Prompt: "Summarize relevant mail"},
	}
	e, err := newEngine(filepath.Join(t.TempDir(), "state.json"), rules)
	if err != nil {
		t.Fatal(err)
	}
	for i, ev := range []Event{{"one", "desktop.usage", map[string]any{"seconds": 7300, "app": "editor"}}, {"two", "calendar.date", map[string]any{"month": 9, "day": 24}}, {"three", "mail.changed", map[string]any{"labels": []string{"inbox"}, "sender": "person@example.test"}}} {
		n, err := e.receive(t.Context(), ev, now)
		if err != nil || n != 1 {
			t.Fatalf("case %d: %d %v", i, n, err)
		}
		n, err = e.receive(t.Context(), ev, now)
		if err != nil || n != 0 {
			t.Fatalf("duplicate: %d %v", n, err)
		}
	}
	locked, unlocked := false, true
	for _, p := range []Presence{{true, nil}, {true, &locked}, {false, &unlocked}} {
		n, err := e.deliver(now, p, func(Activation) error { t.Fatal("presence bypass"); return nil })
		if n != 0 || err != nil {
			t.Fatal(n, err)
		}
	}
	n, err := e.deliver(now, Presence{true, &unlocked}, func(Activation) error { return nil })
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	e, err = newEngine(e.file, rules)
	if err != nil {
		t.Fatal(err)
	}
	n, err = e.deliver(now, Presence{true, &unlocked}, func(Activation) error { t.Fatal("redelivery after restart"); return nil })
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	n, err = e.receive(t.Context(), Event{"four", "desktop.usage", map[string]any{"seconds": 7400, "app": "editor"}}, now)
	if err != nil || n != 0 {
		t.Fatal("cooldown", n, err)
	}
}
func TestUnknownDispatchAndRuleReplacement(t *testing.T) {
	r := Rule{ID: "generic", On: "custom", When: `event.ready == true`, Prompt: "Work"}
	now := time.Now()
	yes := true
	e, err := newEngine(filepath.Join(t.TempDir(), "state.json"), []Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.receive(t.Context(), Event{"1", "custom", map[string]any{"ready": true}}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.deliver(now, Presence{true, &yes}, func(Activation) error { return errors.New("lost receipt") })
	if err == nil {
		t.Fatal("expected unknown")
	}
	e, err = newEngine(e.file, []Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.deliver(now, Presence{true, &yes}, func(Activation) error { t.Fatal("uncertain dispatch retried"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.receive(t.Context(), Event{"2", "custom", map[string]any{"ready": true}}, now)
	if err != nil {
		t.Fatal(err)
	}
	r.Prompt = "Changed intent"
	e, err = newEngine(e.file, []Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.deliver(now, Presence{true, &yes}, func(Activation) error { t.Fatal("stale rule dispatched"); return nil })
	if err != nil {
		t.Fatal(err)
	}
}
func TestExpressionAndInputBounds(t *testing.T) {
	for _, expr := range []string{`exec('touch /tmp/unsafe')`, `event.ready = true`, `'not bool'`, strings.Repeat("!", 2100) + "true"} {
		if _, err := newEngine(filepath.Join(t.TempDir(), "state"), []Rule{{ID: "x", On: "x", When: expr, Prompt: "x"}}); err == nil {
			t.Fatalf("accepted %s", expr[:min(50, len(expr))])
		}
	}
	e, err := newEngine(filepath.Join(t.TempDir(), "state"), []Rule{{ID: "x", On: "x", When: `event.values.all(a, event.values.all(b, a == b))`, Prompt: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	vals := make([]int, 500)
	if _, err = e.receive(context.Background(), Event{"1", "x", map[string]any{"values": vals}}, time.Now()); err == nil {
		t.Fatal("cost budget not enforced")
	}
	if _, err = e.receive(context.Background(), Event{"2", "x", map[string]any{"body": strings.Repeat("x", 17000)}}, time.Now()); err == nil {
		t.Fatal("input bound not enforced")
	}
}

func TestExpiryAndEventCannotGrantPresence(t *testing.T) {
	now := time.Now()
	e, err := newEngine(filepath.Join(t.TempDir(), "state"), []Rule{{ID: "x", On: "trusted.source", When: `event.ready == true`, Prompt: "Work"}})
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.receive(t.Context(), Event{"1", "wrong.source", map[string]any{"ready": true}}, now)
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
	n, err = e.receive(t.Context(), Event{"1", "trusted.source", map[string]any{"ready": true, "unlocked": true}}, now)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = e.deliver(now, Presence{Awake: true}, func(Activation) error { t.Fatal("event forged presence"); return nil }); err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err = e.deliver(now.Add(2*time.Hour), Presence{true, &yes}, func(Activation) error { t.Fatal("expired activation delivered"); return nil }); err != nil {
		t.Fatal(err)
	}
	if e.state.Entries[0].Status != "expired" {
		t.Fatal(e.state)
	}
}
