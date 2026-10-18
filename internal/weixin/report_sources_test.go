package weixin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/activation"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func TestReportRoutingRequiresExactPersistedSource(t *testing.T) {
	for _, scenario := range []struct {
		name, reason string
		change       func(*Bridge, *api.Snapshot)
	}{
		{"missing_metadata", "turn_source_missing", func(b *Bridge, s *api.Snapshot) { s.ReportSources = nil }},
		{"missing_task", "report_source_missing", func(b *Bridge, s *api.Snapshot) { delete(b.state.TaskTurns, "task") }},
		{"unknown_report", "report_source_missing", func(b *Bridge, s *api.Snapshot) { s.ReportSources[0].ReportID = "unknown" }},
		{"missing_origin", "report_source_missing", func(b *Bridge, s *api.Snapshot) { delete(b.state.TurnOrigins, "original") }},
		{"missing_scope", "task_scope_mismatch", func(b *Bridge, s *api.Snapshot) { delete(b.state.TurnOwners, "original") }},
		{"other_thread", "task_scope_mismatch", func(b *Bridge, s *api.Snapshot) { b.state.TurnOwners["original"] = "other-thread" }},
		{"other_runtime", "report_scope_mismatch", func(b *Bridge, s *api.Snapshot) { s.ReportSources[0].RuntimeOwner = "other-runtime" }},
		{"unscoped_cached_completion", "turn_scope_missing", func(b *Bridge, s *api.Snapshot) { b.state.TurnOrigins["completion"] = "weixin" }},
		{"unscoped_cached_completion_with_native_input", "turn_scope_missing", func(b *Bridge, s *api.Snapshot) {
			b.state.TurnOrigins["completion"] = "weixin"
			s.InputSources = []api.InputSource{{RuntimeOwner: "runtime", TurnKey: "completion", RequestID: "original-request"}}
		}},
		{"native_input_wrong_scope", "input_scope_mismatch", func(b *Bridge, s *api.Snapshot) {
			s.InputSources = []api.InputSource{{RuntimeOwner: "other-runtime", TurnKey: "completion", RequestID: "original-request"}}
		}},
		{"cached_different_thread", "runtime_mismatch", func(b *Bridge, s *api.Snapshot) {
			b.state.TurnOwners["completion"], b.state.TurnOrigins["completion"] = "other-thread", "weixin"
		}},
		{"cached_missing_runtime", "runtime_mismatch", func(b *Bridge, s *api.Snapshot) {
			b.state.TurnOwners["completion"], b.state.TurnOrigins["completion"] = "runtime", "weixin"
			s.RuntimeOwner, s.ReportSources = "", nil
		}},
		{"desktop_task", "", func(b *Bridge, s *api.Snapshot) { b.state.TurnOrigins["original"] = "other" }},
		{"mixed_task", "", func(b *Bridge, s *api.Snapshot) { b.state.TurnOrigins["original"] = "mixed" }},
		{"desktop_steer", "", func(b *Bridge, s *api.Snapshot) {
			s.Items = append(s.Items, api.Item{ID: "desktop-input", Kind: "user", TurnKey: "completion", RequestID: "desktop-request"})
		}},
		{"repaired_peer", "report_source_missing", func(b *Bridge, s *api.Snapshot) {
			b.state = emptyDocument()
			b.state.OwnerID = "different-private-peer"
			freshWindow(b, "new-input")
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: "runtime", Phase: "completed", ReportSources: []api.ReportSource{{RuntimeOwner: "runtime", TurnKey: "completion", ReportID: "report", Phase: "completed"}}, Items: []api.Item{
				{ID: "hidden", Kind: "hostNotice", TurnKey: "completion", RequestID: "report", Text: "PRIVATE notice token secret"},
				{ID: "reply", Kind: "assistant", Status: "completed", TurnKey: "completion", Text: "PRIVATE final text"},
			}}
			b, err := Open(root, Host{Snapshot: func() api.Snapshot { return activation.Present(snapshot, nil, false) }, TaskForReport: func(id string) string {
				if id == "report" {
					return "task"
				}
				return ""
			}, Diagnostics: diagnosticlog.New(filepath.Join(root, "logs"))})
			if err != nil {
				t.Fatal(err)
			}
			b.state.OwnerID = "private-peer"
			freshWindow(b, "original-request")
			b.state.TaskTurns["task"] = "original"
			b.state.TurnOrigins["original"], b.state.TurnOwners["original"] = "weixin", "runtime"
			scenario.change(b, &snapshot)
			server, texts := sentTexts(t, nil)
			defer server.Close()
			p := &protocol{client: server.Client(), base: server.URL, token: "private-token"}
			for range 3 {
				b.output(t.Context(), p)
			}
			if len(*texts) != 0 || len(b.state.Outputs) != 0 || len(b.state.Progress) != 0 || b.state.Window.Used != 0 {
				t.Fatal("unproven source reached outbox")
			}
			if scenario.reason != "" {
				data, err := os.ReadFile(filepath.Join(root, "logs", "error.jsonl"))
				if err != nil {
					t.Fatal("missing route diagnostic", err)
				}
				log := string(data)
				if strings.Count(log, "route_source_unresolved") != 1 || !strings.Contains(log, scenario.reason) {
					t.Fatal("diagnostic missing or not deduplicated", log)
				}
				for _, secret := range []string{"PRIVATE", "private-peer", "private-token", "original-request", "completion", "\"runtime\""} {
					if strings.Contains(log, secret) {
						t.Fatal("private routing input leaked into diagnostics")
					}
				}
			}
		})
	}
}
