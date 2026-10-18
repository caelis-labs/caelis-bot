package weixin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// Regression for PR #193: legacy ledgers must never acquire the current scope
// merely because persisted chat includes an old task card or completion.
func TestLegacySourceCannotAdoptCurrentScope(t *testing.T) {
	for _, scenario := range []struct {
		name, runtime string
		taskCard      bool
	}{
		{"cached_completion", "different-conversation", false},
		{"source_task_card_in_chat", "different-conversation", true},
		{"cached_completion_missing_runtime", "", false},
		{"source_task_card_missing_runtime", "", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot := api.Snapshot{Connection: "ready", RuntimeOwner: scenario.runtime, Phase: "completed", Items: []api.Item{
				{ID: "completion-answer", Kind: "assistant", Status: "completed", TurnKey: "completion", Text: "private task result"},
			}}
			root := t.TempDir()
			b, err := Open(root, Host{Snapshot: func() api.Snapshot { return snapshot }, TaskForReport: func(id string) string {
				if id == "report" {
					return "task"
				}
				return ""
			}})
			if err != nil {
				t.Fatal(err)
			}
			b.state.OwnerID = "fixture-peer"
			freshWindow(b, "fresh-unrelated-request")
			// A pre-upgrade ledger has persisted origins, but no TurnOwners.
			if scenario.taskCard {
				b.state.TaskTurns["task"] = "original"
				b.state.TurnOrigins["original"] = "weixin"
				snapshot.ReportSources = []api.ReportSource{{RuntimeOwner: snapshot.RuntimeOwner, TurnKey: "completion", ReportID: "report", Phase: "completed"}}
				snapshot.Items = append([]api.Item{{ID: "task-start:task", Kind: "assistant", Status: "completed", TurnKey: "original", Text: "Worker started", Task: &api.TaskPresentation{ID: "task", Title: "Worker started"}}}, snapshot.Items...)
				b.state.Outputs["item:task-start:task"] = outbound{State: "skip"}
			} else {
				b.state.TurnOrigins["completion"] = "weixin"
				b.state.TurnPhases["completion"] = "completed"
			}
			if err := b.saveLocked(); err != nil {
				t.Fatal(err)
			}
			b, err = Open(root, b.host)
			if err != nil {
				t.Fatal(err)
			}
			server, texts := sentTexts(t, nil)
			defer server.Close()
			p := &protocol{client: server.Client(), base: server.URL, token: "fixture"}
			for range 2 {
				for range 3 {
					b.output(t.Context(), p)
				}
				if len(*texts) != 0 || len(b.state.TurnOwners) != 0 || len(b.state.Progress) != 0 || b.state.Window.Used != 0 {
					t.Fatal("legacy source acquired scope or reached the outbox")
				}
				b, err = Open(root, b.host)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestHistoricalChatCannotEstablishInputScope(t *testing.T) {
	for _, origin := range []string{"weixin", ""} {
		for _, storedOwner := range []string{"", "old-conversation"} {
			t.Run(fmt.Sprintf("origin=%s/owner=%s", origin, storedOwner), func(t *testing.T) {
				root := t.TempDir()
				// Only a newer desktop input belongs to this native conversation.
				engine := inputSnapshotEngine{value: api.Snapshot{Connection: "ready", RuntimeOwner: "current-conversation", Phase: "completed",
					InputSources:  []api.InputSource{{RuntimeOwner: "current-conversation", TurnKey: "desktop-turn", RequestID: "desktop-request"}},
					Items:         []api.Item{{ID: "desktop", Kind: "user", TurnKey: "desktop-turn", RequestID: "desktop-request"}},
					ReportSources: []api.ReportSource{{RuntimeOwner: "current-conversation", TurnKey: "completion", ReportID: "report", Phase: "completed"}},
				}}
				service := backend.NewService(engine, nil, nil, nil, nil)
				chatPath := filepath.Join(root, "chat.sqlite")
				service.ConfigureChat(chatPath)
				service.ObserveChat(api.Snapshot{Items: []api.Item{
					{ID: "historical-input", Kind: "user", Status: "completed", TurnKey: "original", RequestID: "old-wx-request", Text: "PRIVATE historical request"},
					{ID: "task-start:task", Kind: "assistant", Status: "completed", TurnKey: "original", Text: "PRIVATE task", Task: &api.TaskPresentation{ID: "task", Title: "PRIVATE task"}},
					{ID: "completion-answer", Kind: "assistant", Status: "completed", TurnKey: "completion", Text: "PRIVATE result"},
				}})
				if err := service.Shutdown(); err != nil {
					t.Fatal(err)
				}
				service = backend.NewService(engine, nil, nil, nil, nil)
				service.ConfigureChat(chatPath)
				defer service.Shutdown()
				deadline := time.Now().Add(5 * time.Second)
				for len(service.Snapshot().Items) < 3 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if len(service.Snapshot().Items) != 3 || len(service.Snapshot().InputSources) != 1 {
					t.Fatal("test did not assemble restored chat with native input evidence")
				}
				host := Host{Snapshot: service.Snapshot, TaskForReport: func(string) string { return "task" }, Diagnostics: diagnosticlog.New(filepath.Join(root, "logs"))}
				b, err := Open(root, host)
				if err != nil {
					t.Fatal(err)
				}
				b.state.OwnerID = "private-peer"
				freshWindow(b, "unrelated-fresh-request")
				b.state.Inputs["old-wx-request"] = "accepted"
				b.state.TaskTurns["task"] = "original"
				b.state.TurnOrigins["original"] = origin
				b.state.TurnOwners["original"] = storedOwner
				if err := b.saveLocked(); err != nil {
					t.Fatal(err)
				}
				server, texts := sentTexts(t, nil)
				defer server.Close()
				for range 2 {
					b, err = Open(root, host)
					if err != nil {
						t.Fatal(err)
					}
					for range 3 {
						b.output(t.Context(), &protocol{client: server.Client(), base: server.URL, token: "private-token"})
					}
					if len(*texts) != 0 || len(b.state.Outputs) != 0 || len(b.state.Progress) != 0 || b.state.Window.Used != 0 || b.state.TurnOwners["original"] != storedOwner || b.state.TurnOwners["completion"] != "" {
						t.Fatal("persisted chat established scope or reached the outbox")
					}
				}
				data, err := os.ReadFile(filepath.Join(root, "logs", "error.jsonl"))
				if err != nil || strings.Count(string(data), "route_source_unresolved") != 4 {
					t.Fatal("expected one deduplicated diagnostic per turn and bridge instance", err)
				}
				for _, secret := range []string{"PRIVATE", "private-peer", "private-token", "old-wx-request", "current-conversation", "old-conversation"} {
					if strings.Contains(string(data), secret) {
						t.Fatal("private routing value entered diagnostics")
					}
				}
			})
		}
	}
}

func TestSourceWriteFailureRollsBack(t *testing.T) {
	root := t.TempDir()
	b, err := Open(root, Host{TaskForReport: func(string) string { return "task" }, Diagnostics: diagnosticlog.New(filepath.Join(root, "logs"))})
	if err != nil {
		t.Fatal(err)
	}
	b.state.TaskTurns["task"] = "original"
	b.state.TurnOrigins["original"], b.state.TurnOwners["original"] = "weixin", "runtime"
	b.path = t.TempDir() // deliberately make persistence target a directory
	s := api.Snapshot{RuntimeOwner: "runtime", ReportSources: []api.ReportSource{{RuntimeOwner: "runtime", TurnKey: "completion", ReportID: "report"}}}
	if b.ownsTurn(s, "completion") {
		t.Fatal("failed storage authorized a route")
	}
	if b.state.TurnOrigins["completion"] != "" || b.state.TurnOwners["completion"] != "" {
		t.Fatal("failed storage retained inferred source")
	}
	data, err := os.ReadFile(filepath.Join(root, "logs", "error.jsonl"))
	if err != nil || !strings.Contains(string(data), "source_storage_failed") {
		t.Fatal("missing storage diagnostic", err)
	}
}

func TestRouteDiagnosticsAreBounded(t *testing.T) {
	root := t.TempDir()
	b, err := Open(root, Host{Diagnostics: diagnosticlog.New(filepath.Join(root, "logs"))})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 300 {
		if b.ownsTurn(api.Snapshot{RuntimeOwner: "fixture-runtime"}, fmt.Sprintf("fixture-turn-%d", i)) {
			t.Fatal("unproven route accepted")
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "logs", "error.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.routeWarnings) != 256 || strings.Count(string(data), "route_source_unresolved") != 256 {
		t.Fatal("unbounded or missing diagnostics")
	}
	if strings.Contains(string(data), "fixture-runtime") || strings.Contains(string(data), "fixture-turn") {
		t.Fatal("raw source identity in diagnostics")
	}
}
