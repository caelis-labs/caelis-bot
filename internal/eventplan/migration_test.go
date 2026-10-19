package eventplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func legacyFixture(t *testing.T, now time.Time, status string) (string, []byte) {
	t.Helper()
	s := legacyState{Version: 2, Policy: DefaultPolicy(), Watermarks: map[string]time.Time{"clock.minute": now.Add(-time.Minute)}, Rules: []legacyRule{{ID: "original", Label: "Original", On: "clock.minute", When: "true", Prompt: "Report the requested result", TimeZone: "UTC", Version: "original-version", Enabled: true, CooldownSeconds: 60, ExpiresSeconds: 900, OriginRequests: []string{"original-phone-input"}}}, Activations: []legacyActivation{{ID: "original-activation", RuleID: "original", Version: "original-version", Prompt: "Report the requested result", Status: status, Expires: now.Add(15 * time.Minute), OriginRequests: []string{"original-phone-input"}}}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "care-fixture.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}
func TestLegacyMigrationAtomicIdempotentAndReadOnly(t *testing.T) {
	e, now := setup(t, nil)
	path, raw := legacyFixture(t, *now, "pending")
	if err := e.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	original := e.Snapshot()
	if original.Migration == nil || len(original.Plans) != 1 || original.Occurrences[0].ID != "original-activation" || original.Plans[0].Authorization() != "care:original" {
		t.Fatal(original)
	}
	if err := e.MigrateLegacy(path, now.Add(time.Minute)); err != nil || !reflect.DeepEqual(original, e.Snapshot()) {
		t.Fatal("migration repeated", err)
	}
	disk, _ := os.ReadFile(path)
	if !bytes.Equal(disk, raw) {
		t.Fatal("legacy file mutated")
	}
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	// A completed import must not depend on the retired file remaining readable.
	if err = os.WriteFile(path, []byte("invalid later bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = restored.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	count := 0
	d := noDelivery()
	d.Ready = true
	d.Submit = func(_ context.Context, id, text, plan string) (string, error) {
		count++
		if id != "original-activation" {
			t.Fatal("new identity", id)
		}
		return "accepted", nil
	}
	tick(t, restored, *now, d)
	tick(t, restored, *now, d)
	if count != 1 || restored.Snapshot().Occurrences[0].Status != "accepted" {
		t.Fatal("pending was lost or duplicated", count)
	}
}
func TestLegacyUnknownAndInterruptedNeverReplay(t *testing.T) {
	for _, status := range []string{"unknown", "dispatching", "accepted"} {
		t.Run(status, func(t *testing.T) {
			e, now := setup(t, nil)
			path, _ := legacyFixture(t, *now, status)
			if err := e.MigrateLegacy(path, *now); err != nil {
				t.Fatal(err)
			}
			restored, err := Open(e.path, nil, *now)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			d := noDelivery()
			d.Ready = true
			d.Submit = func(context.Context, string, string, string) (string, error) {
				t.Fatal("uncertain operation replayed")
				return "", nil
			}
			d.Receipt = func(id string) string {
				if id != "original-activation" {
					t.Fatal(id)
				}
				return "unknown"
			}
			for range 3 {
				tick(t, restored, *now, d)
			}
			d.Receipt = func(string) string { return "accepted" }
			d.Complete = func(id string) bool { return id == "original-activation" }
			tick(t, restored, *now, d)
			if restored.Snapshot().Occurrences[0].Status != "completed" {
				t.Fatal("original completion not reconciled")
			}
		})
	}
}
func TestMigrationCommitFailureAndCorruptSourcePreserveEvidence(t *testing.T) {
	e, now := setup(t, nil)
	path, raw := legacyFixture(t, *now, "pending")
	e.write = func(string, any) error { return errors.New("synthetic disk failure") }
	if e.MigrateLegacy(path, *now) == nil {
		t.Fatal("write failure hidden")
	}
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if restored.Snapshot().Migration != nil {
		t.Fatal("partial checkpoint committed")
	}
	if err = restored.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(raw, data) {
		t.Fatal("source changed")
	}
	e2, _ := setup(t, nil)
	bad := filepath.Join(t.TempDir(), "care.json")
	os.WriteFile(bad, []byte(`{"version":99}`), 0600)
	if e2.MigrateLegacy(bad, *now) == nil || e2.Snapshot().Migration != nil {
		t.Fatal("invalid source acknowledged")
	}
	if _, err = e2.Save(Plan{ID: "independent", Event: "mail", Kind: "after", Seconds: 60}, *now); err != nil {
		t.Fatal("bad legacy data blocked independent schedules", err)
	}
}
func TestMigrationExpiryCancellationVersionAndWatermark(t *testing.T) {
	for _, change := range []string{"expired", "cancelled", "old-version"} {
		t.Run(change, func(t *testing.T) {
			e, now := setup(t, nil)
			path, raw := legacyFixture(t, *now, "pending")
			var s legacyState
			json.Unmarshal(raw, &s)
			switch change {
			case "expired":
				s.Activations[0].Expires = *now
			case "cancelled":
				s.Rules[0].Enabled = false
			case "old-version":
				s.Activations[0].Version = "superseded"
			}
			raw, _ = json.Marshal(s)
			os.WriteFile(path, raw, 0600)
			if err := e.MigrateLegacy(path, *now); err != nil {
				t.Fatal(err)
			}
			if e.Snapshot().Occurrences[0].Finished.IsZero() {
				t.Fatal("terminal pending became executable")
			}
			if err := e.ReceiveNative(t.Context(), []scheduledata.Event{{Source: "clock.minute", At: now.Add(-time.Minute), Data: map[string]any{}}}, *now); err != nil {
				t.Fatal(err)
			}
			if len(e.Snapshot().Occurrences) != 1 {
				t.Fatal("old source watermark replayed")
			}
		})
	}
}
func TestNativeUsesUnifiedBusyBudgetAndConditions(t *testing.T) {
	e, now := setup(t, nil)
	p, err := e.SaveNative(NativeArrangement{ID: "native", Label: "Native", Prompt: "Follow up", Sources: []string{"clock.minute"}, When: "event.unlocked == true", TimeZone: "UTC", ExpiresSeconds: 60}, *now, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, unlocked := range []any{nil, false, true} {
		at := now.Add(time.Duration(i) * time.Minute)
		if err = e.ReceiveNative(t.Context(), []scheduledata.Event{{Source: "clock.minute", At: at, Data: map[string]any{"unlocked": unlocked}}}, at); err != nil {
			t.Fatal(err)
		}
	}
	s := e.Snapshot()
	if len(s.Occurrences) != 3 || s.Occurrences[0].Status != "filtered" || s.Occurrences[1].Status != "filtered" || s.Occurrences[2].Status != "pending" {
		t.Fatal(s)
	}
	at := now.Add(2 * time.Minute)
	d := noDelivery()
	count := 0
	d.Submit = func(context.Context, string, string, string) (string, error) { count++; return "deferred", nil }
	tick(t, e, at, d)
	if count != 0 {
		t.Fatal("busy dispatched")
	}
	d.Ready = true
	tick(t, e, at, d)
	if len(e.Snapshot().WakeAttempts) != 0 || !e.Snapshot().Plans[0].LastWake.IsZero() {
		t.Fatal("deferred consumed quota")
	}
	id := s.Occurrences[2].ID
	d.Submit = func(_ context.Context, got, _, plan string) (string, error) {
		count++
		if got != id || plan != p.ID {
			t.Fatal("identity changed")
		}
		return "accepted", nil
	}
	tick(t, e, at, d)
	tick(t, e, at, d)
	if count != 2 || len(e.Snapshot().WakeAttempts) != 1 {
		t.Fatal("duplicate native submission")
	}
}
func TestMigratedUnknownCanSettleFromOriginalCompletion(t *testing.T) {
	e, now := setup(t, nil)
	path, _ := legacyFixture(t, *now, "unknown")
	if err := e.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	d := noDelivery()
	d.Complete = func(id string) bool { return id == "original-activation" }
	tick(t, e, *now, d)
	if e.Snapshot().Occurrences[0].Status != "completed" {
		t.Fatal("original completion evidence ignored")
	}
}
func TestLegacyV1BudgetAndSharedGrantArePreserved(t *testing.T) {
	e, now := setup(t, nil)
	path, raw := legacyFixture(t, *now, "accepted")
	var s legacyState
	json.Unmarshal(raw, &s)
	s.Version = 1
	s.Policy = Policy{4, 600}
	s.Attempts = []time.Time{now.Add(-time.Minute)}
	s.Activations = append(s.Activations, legacyActivation{ID: "old-unknown", RuleID: "original", Version: "older-version", Status: "unknown"})
	raw, _ = json.Marshal(s)
	os.WriteFile(path, raw, 0600)
	if err := e.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	state := e.Snapshot()
	if state.Policy != (Policy{4, 600}) || len(state.WakeAttempts) == 0 || state.Occurrences[0].Status != "completed" {
		t.Fatal("v1 accounting or acceptance lost", state)
	}
	d := noDelivery()
	d.Receipt = func(string) string { return "rejected" }
	d.Revoke = func(id string) error {
		if id == "care:original" {
			t.Fatal("orphan receipt revoked an active arrangement grant")
		}
		return nil
	}
	tick(t, e, *now, d)
}
func TestMigrationPreservesSameTimeQuotaWithoutInventingDispatch(t *testing.T) {
	e, now := setup(t, nil)
	path, raw := legacyFixture(t, *now, "unknown")
	var s legacyState
	json.Unmarshal(raw, &s)
	at := now.Add(-time.Hour)
	s.Attempts = []time.Time{at, at}
	s.Interruptions = []time.Time{at, at}
	raw, _ = json.Marshal(s)
	os.WriteFile(path, raw, 0600)
	if err := e.MigrateLegacy(path, *now); err != nil {
		t.Fatal(err)
	}
	state := e.Snapshot()
	if len(state.WakeAttempts) != 2 || len(state.WakeReservations) != 1 || !state.WakeAttempts[1].Equal(at) {
		t.Fatal("quota collapsed or migration invented a dispatch", state)
	}
}
func TestMigrationRejectsNonRegularAndOversizedSources(t *testing.T) {
	for _, kind := range []string{"directory", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			e, now := setup(t, nil)
			path := filepath.Join(t.TempDir(), "care.json")
			if kind == "directory" {
				os.Mkdir(path, 0700)
			} else {
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				f.Truncate((2 << 20) + 1)
				f.Close()
			}
			if e.MigrateLegacy(path, *now) == nil || e.Snapshot().Migration != nil {
				t.Fatal("invalid source acknowledged")
			}
		})
	}
}
func TestInvalidOrphanCannotPoisonDestinationOnRestart(t *testing.T) {
	e, now := setup(t, nil)
	path, raw := legacyFixture(t, *now, "unknown")
	var s legacyState
	json.Unmarshal(raw, &s)
	s.Activations[0].Version = "superseded"
	s.Activations[0].Prompt = string(bytes.Repeat([]byte("x"), 4097))
	raw, _ = json.Marshal(s)
	os.WriteFile(path, raw, 0600)
	before := e.Snapshot()
	if e.MigrateLegacy(path, *now) == nil || !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("invalid imported record poisoned destination")
	}
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	restored.Close()
}
