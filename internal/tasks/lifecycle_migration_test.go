package tasks

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyUnavailablePreservesOriginalTaskAndObservedReport(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tasks.json")
	f := newRuntime()
	old := record{Provider: "codex", View: api.Task{ID: "original-task", Status: "unavailable"}, Execution: "original-run", ReportID: "original-report", ReportState: "observed", Requests: []string{"original-request"}, RequestOutcomes: map[string]string{"original-request": "unknown"}}
	raw, _ := json.Marshal(state{Version: 1, Records: map[string]*record{"original-task": &old}})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.state.Records["original-task"]; got.View.Status != "unknown" || got.ReportID != "original-report" || got.ReportState != "observed" || got.RequestOutcomes["original-request"] != "unknown" {
		t.Fatal("legacy receipts lost", got)
	}
	f.states["original-task"] = api.WorkState{Task: api.Task{ID: "original-task", Status: "completed"}, ExecutionKey: "original-run", Activity: "idle"}
	if err := m.DeliverTaskReport(t.Context()); err != nil || f.reports != 0 || f.starts != 0 {
		t.Fatal("migration replayed an observed report", err)
	}
	if got := m.state.Records["original-task"]; got.ReportID != "original-report" || got.ReportState != "observed" {
		t.Fatal("report identity changed")
	}
}
