package eventplan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpgradePreservesClockCadenceAndOriginalReceipts(t *testing.T) {
	e, now := setup(t, nil)
	raw := []byte(`{
		"version":1,
		"migration":{"version":1,"digest":"old-import","completed":"2026-10-01T00:00:00Z"},
		"events":[{"id":"clock","label":"Periodic clock","kind":"wake"},
			{"id":"old","label":"Legacy plugin","kind":"tool","arguments":{"old":"sensitive collector args"}},
			{"id":"native","label":"Desktop observer","kind":"native"}],
		"plans":[
			{"id":"repeat","event":"clock","kind":"interval","seconds":60,"cooldownSeconds":3600,"status":"active","created":"2026-10-19T08:00:00Z","next":"2026-10-19T08:01:00Z","expires":"2026-11-18T08:00:00Z"},
			{"id":"old","event":"old","kind":"interval","seconds":60,"status":"active","created":"2026-10-19T08:00:00Z","next":"2026-10-19T08:00:00Z"},
			{"id":"native","event":"native","kind":"native","grantId":"care:old","status":"active","created":"2026-10-19T08:00:00Z","next":"2026-10-19T08:00:00Z"}],
		"occurrences":[
			{"id":"occurrence:unsent","plan":"old","event":"old","status":"pending","at":"2026-10-19T08:00:00Z"},
			{"id":"original-activation","plan":"native","event":"native","status":"dispatching","at":"2026-10-19T08:00:00Z"}]}`)
	if err := os.WriteFile(e.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	s := restored.Snapshot()
	if len(s.Events) != 1 || s.Events[0].ID != "clock" || s.Occurrences[0].Status != "cancelled" || s.Occurrences[1].Status != "unknown_wake" {
		t.Fatal("old collector could still run or receipt was discarded", s)
	}
	settled, wakes, revoked := false, 0, 0
	d := Delivery{
		Ready: func() bool { return true },
		Submit: func(_ context.Context, w Wake) (string, error) {
			if w.Plan != "repeat" {
				t.Fatal("legacy collector was replayed", w)
			}
			wakes++
			return "accepted", nil
		},
		Receipt: func(id string) string {
			if id != "original-activation" {
				t.Fatal("wrong receipt", id)
			}
			if settled {
				return "accepted"
			}
			return "unknown"
		},
		Revoke: func(id string) error {
			if id == "care:old" {
				if !settled {
					t.Fatal("grant revoked before original receipt")
				}
				revoked++
			}
			return nil
		},
	}
	tick(t, restored, now.Add(time.Minute), d)
	settled = true
	tick(t, restored, now.Add(2*time.Minute), d)
	tick(t, restored, now.Add(2*time.Minute), d)
	if wakes != 2 || revoked != 1 {
		t.Fatalf("clock=%d, grant=%d", wakes, revoked)
	}
	upgraded, err := os.ReadFile(e.path)
	if err != nil || strings.Contains(string(upgraded), "sensitive collector args") || strings.Contains(string(upgraded), "cooldownSeconds") {
		t.Fatal("legacy execution payload stayed in the active ledger", err)
	}
	if reopened, err := Open(e.path, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	} else {
		reopened.Close()
	}
}

func TestCareImportIsReadOnlyAndKeepsOutstandingIDs(t *testing.T) {
	e, now := setup(t, nil)
	path := filepath.Join(t.TempDir(), "care.json")
	old := []byte(`{"version":2,"rules":[{"id":"old"}],"activations":[
		{"id":"original-accepted","ruleId":"old","status":"accepted"},
		{"id":"original-unknown","ruleId":"old","status":"unknown"},
		{"id":"never-dispatched","ruleId":"old","status":"pending"}]}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := e.MigrateLegacy(path, *now); err != nil {
			t.Fatal(err)
		}
	}
	s := e.Snapshot()
	if len(s.Events) != 0 || len(s.Occurrences) != 2 || s.Occurrences[0].ID != "original-accepted" || s.Occurrences[1].ID != "original-unknown" {
		t.Fatal("import changed the recoverable scope", s)
	}
	source, _ := os.ReadFile(path)
	if string(source) != string(old) {
		t.Fatal("legacy source altered")
	}
	raw, err := os.ReadFile(e.path)
	var saved State
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.Migration == nil {
		t.Fatal("migration lost its checkpoint", err)
	}
}
