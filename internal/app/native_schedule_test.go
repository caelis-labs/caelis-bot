package app

import (
	"bytes"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredStoreFailurePreservesChatAndIndependentSchedules(t *testing.T) {
	e := newTestEngine()
	var reported []error
	a, root := fixtureApp(t, e, Host{ScheduleSample: func() scheduledata.Sample { return scheduledata.Sample{} }, ReportError: func(err error) { reported = append(reported, err) }})
	path := filepath.Join(root, "care-fixture.json")
	raw := []byte(`{"version":99}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if a.personal == nil || a.notebook == nil || a.skillPath == "" || len(reported) != 1 {
		t.Fatal("migration error broke personal preparation")
	}
	if out := a.companion.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(`{"request":{"type":"after","id":"independent","seconds":60,"context":"Follow up"}}`)); out.IsError {
		t.Fatal("legacy error blocked ordinary event schedule", out)
	}
	if out := a.companion.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(`{"request":{"type":"save","id":"blocked","label":"Blocked","prompt":"Follow up","trigger":{"type":"event","sources":["clock.minute"],"condition":"true","timeZone":"UTC"}}}`)); !out.IsError {
		t.Fatal("native registration bypassed unresolved migration")
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitSignal(t, e.connectSeen)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(raw, got) {
		t.Fatal("retired ledger changed")
	}
}
