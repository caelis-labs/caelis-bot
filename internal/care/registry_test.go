package care

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestExtensionJSONUsesSameCELQueueAndCannotForgePresence(t *testing.T) {
	source := Source{Name: "github.pullRequests", Description: "Registered PR collector", Fields: map[string]string{"pullRequests": "array of number, title, reviewRequested"}}
	e, err := OpenWithSources(filepath.Join(t.TempDir(), "state.json"), append(NativeSources(), source))
	if err != nil {
		t.Fatal(err)
	}
	source.Fields["injected"] = "field"
	listed := e.Sources()
	listed[0].Fields["injected"] = "another"
	if _, ok := e.Sources()[0].Fields["injected"]; ok {
		t.Fatal("mutable catalog")
	}
	r := rule("review")
	r.On = source.Name
	r.When = "event.pullRequests.exists(pr, pr.reviewRequested && pr.number > 0)"
	save(t, e, r)
	event := Event{Source: source.Name, At: instant, Data: map[string]any{"pullRequests": []any{map[string]any{"number": 42.0, "reviewRequested": true}}, "unlocked": true}}
	if err = e.Receive(t.Context(), event, instant); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Activations) != 1 {
		t.Fatal("adapter JSON did not match")
	}
	_ = e.Deliver(t.Context(), instant, Presence{Awake: true}, true, noReceipt, func(context.Context, Activation) (api.Receipt, error) {
		t.Fatal("event spoofed host presence")
		return api.Receipt{}, nil
	})
	// Removing an adapter preserves its rule but suppresses already queued work.
	missing, err := Open(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Snapshot().Rules[0].Issue != "source_unavailable" {
		t.Fatal("missing adapter not explained")
	}
	deliver(t, missing, instant)
	if missing.Snapshot().Activations[0].Status != "cancelled" {
		t.Fatal("missing adapter dispatched")
	}
	if _, err = missing.Test(t.Context(), r, event.Data, instant); err == nil {
		t.Fatal("test invented an adapter")
	}
}
func TestSourceRegistryRejectsUnsupportedDeclarations(t *testing.T) {
	for _, sources := range [][]Source{{{Name: "x", Description: "bad"}}, {{Name: "mail.changed"}}, {{Name: "mail.changed", Description: "one"}, {Name: "mail.changed", Description: "two"}}, make([]Source, 33)} {
		if _, err := OpenWithSources(filepath.Join(t.TempDir(), "state"), sources); err == nil {
			t.Fatal("invalid source registration")
		}
	}
}
func TestRuleCapacityAndSnapshotIsolation(t *testing.T) {
	e := opened(t)
	e.write = func(string, any) error { return nil }
	for i := 0; i < MaxRules; i++ {
		r := rule(string(rune('A' + i)))
		r.ID = "r" + r.ID
		if i >= 26 {
			r.ID = "extra" + string(rune('a'+i-26))
		}
		save(t, e, r)
	}
	if _, err := e.Save(t.Context(), rule("over"), nil); err == nil {
		t.Fatal("rule cap bypass")
	}
	snap := e.Snapshot()
	snap.Rules[0].Prompt = "changed"
	if e.Snapshot().Rules[0].Prompt == "changed" {
		t.Fatal("snapshot aliases live state")
	}
}
func TestClockRollbackNeverReplaysOldEvent(t *testing.T) {
	e := opened(t)
	save(t, e, rule("x"))
	receive(t, e, instant)
	deliver(t, e, instant)
	receive(t, e, instant.Add(-time.Hour))
	if len(e.Snapshot().Activations) != 1 {
		t.Fatal("clock rollback replay")
	}
}
