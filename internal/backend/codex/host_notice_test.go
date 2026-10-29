package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func TestHostReportSubmissionIsHiddenThroughReplayAndHistory(t *testing.T) {
	s, f := sessionPair(t, "hold")
	const notice = "Task task-42 is completed."
	in := api.Submission{ID: "host-report-42", Text: notice}
	if receipt, err := s.SubmitSystem(testContext(t), api.SystemMessage{Source: api.SystemWorkerResult, ID: in.ID, Text: in.Text, OriginRequests: in.OriginRequests}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{
		{ID: "notice", Type: "userMessage", ClientID: in.ID, Content: []nativeInput{{Type: "text", Text: notice}}},
		{ID: "result", Type: "agentMessage", Text: "The requested work is ready."},
	}}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
	got := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "The requested work is ready." {
		t.Fatal("host input leaked or reply hidden", got.Items)
	}
	want := api.ReportSource{RuntimeOwner: got.RuntimeOwner, TurnKey: opaque("run-native"), ReportID: in.ID, Phase: "completed"}
	if len(got.ReportSources) != 1 || got.ReportSources[0] != want || s.RecentSnapshot().ReportSources[0] != want {
		t.Fatal("report source lost through cached/recent projection", got.ReportSources)
	}
	got.ReportSources[0].ReportID = "mutated"
	if s.Snapshot().ReportSources[0] != want {
		t.Fatal("snapshot aliases cached provenance")
	}
	restored := NewSession(s.opts)
	if !restored.binding.HostReports[in.ID] {
		t.Fatal("host provenance was not persisted")
	}
	if got := restored.Snapshot(); len(got.ReportSources) != 1 || got.ReportSources[0] != want {
		t.Fatal("report association did not survive restart", got.ReportSources)
	}
	restored.state.Connection = "ready"
	restored.applyTurn(restored.binding.ThreadID, turn, true)
	restored.update()
	got = restored.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" {
		t.Fatal("history replay leaked host input", got.Items)
	}
	restored.applyTurn(restored.binding.ThreadID, nativeTurn{ID: "human-turn", Status: "completed", Items: []nativeItem{{ID: "human", Type: "userMessage", ClientID: "human-same-text", Content: []nativeInput{{Type: "text", Text: notice}}}}}, true)
	restored.update()
	got = restored.Snapshot()
	if len(got.Items) != 2 || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal("same-text human input was hidden", got.Items)
	}
}

func TestHostReportBindingUsesAcceptedTargetBeforeInputEcho(t *testing.T) {
	s, f := sessionPair(t, "hold")
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			return map[string]any{"turn": nativeTurn{ID: "report-turn", Status: "inProgress"}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	if receipt, err := s.SubmitSystem(testContext(t), api.SystemMessage{Source: api.SystemWorkerResult, ID: "report-receipt", Text: "private notice"}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	got := s.Snapshot()
	if len(got.ReportSources) != 1 || got.ReportSources[0].TurnKey != opaque("report-turn") {
		t.Fatal("accepted target missing without input echo")
	}
	restored := NewSession(s.opts)
	restored.applyTurn(restored.binding.ThreadID, nativeTurn{ID: "report-turn", Status: "completed", Items: []nativeItem{{ID: "reply", Type: "agentMessage", Text: "result"}}}, true)
	restored.update()
	if got := restored.Snapshot(); len(got.ReportSources) != 1 || got.ReportSources[0].Phase != "completed" {
		t.Fatal("summary-only recovery lost terminal source")
	}
	restored.rememberHostReportTurn("report-receipt", "different-turn", "completed")
	if restored.reportSources()[0].TurnKey != opaque("report-turn") {
		t.Fatal("original target overwritten")
	}
	restored.binding.ThreadID = "different-thread"
	if len(restored.reportSources()) != 0 {
		t.Fatal("report crossed native thread")
	}
}

func TestOldProductTaskLedgerImportsExactHostReportID(t *testing.T) {
	s, _ := sessionPair(t, "")
	const taskID, reportID, notice = "task-42", "legacy-ledger-report-42", "Task task-42 is completed."
	s.mu.Lock()
	s.binding.Tasks = map[string]*taskRecord{taskID: {View: api.Task{ID: taskID, Title: "Fixture", Status: "completed", Outcome: "accepted"}, Thread: "worker-42", ReportID: "native-execution-42", ReportState: "delivered"}}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	ledger := filepath.Join(filepath.Dir(s.opts.StateFile), "product-tasks.json")
	data, _ := json.Marshal(map[string]any{"version": 1, "records": map[string]any{taskID: map[string]any{"view": api.Task{ID: taskID, Title: "Fixture", Status: "completed", Outcome: "accepted"}, "provider": "codex", "execution": "native-execution-42", "reportId": reportID, "reportState": "delivered"}}})
	if err := os.WriteFile(ledger, data, 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := tasks.Open(ledger, s.workRoot(), "codex", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	ids := manager.HostReportIDs()
	if len(ids) != 1 || ids[0] != reportID {
		t.Fatal("lost ledger source", ids)
	}
	restored := NewSession(s.opts)
	if restored.binding.HostReports[reportID] {
		t.Fatal("fixture already carried the migrated product ID")
	}
	if err := restored.ImportHostReportIDs(ids); err != nil {
		t.Fatal(err)
	}
	restarted := NewSession(s.opts)
	if !restarted.binding.HostReports[reportID] {
		t.Fatal("migration was not durable")
	}
	restarted.state.Connection = "ready"
	restarted.applyTurn(restarted.binding.ThreadID, nativeTurn{ID: "old-report-turn", Status: "completed", Items: []nativeItem{{ID: "notice", Type: "userMessage", ClientID: reportID, Content: []nativeInput{{Type: "text", Text: notice}}}, {ID: "answer", Type: "agentMessage", Text: "Useful result"}}}, true)
	restarted.update()
	got := restarted.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "assistant" || got.Items[0].Text != "Useful result" {
		t.Fatal("upgraded history leaked", got.Items)
	}
}

func TestHostReportIsHiddenInRecentHumanSummary(t *testing.T) {
	s, _ := sessionPair(t, "")
	const notice = "Task task-42 is completed."
	s.mu.Lock()
	s.binding.HostReports["host-report-42"] = true
	s.applyTurn(s.binding.ThreadID, nativeTurn{ID: "older-turn", Status: "completed", Items: []nativeItem{{ID: "host", Type: "userMessage", ClientID: "host-report-42", Content: []nativeInput{{Type: "text", Text: notice}}}, {ID: "answer", Type: "agentMessage", Text: "Reported result"}}}, true)
	s.applyTurn(s.binding.ThreadID, nativeTurn{ID: "human-turn", Status: "completed", Items: []nativeItem{{ID: "human", Type: "userMessage", ClientID: "human-request", Content: []nativeInput{{Type: "text", Text: notice}}}}}, true)
	s.update()
	s.mu.Unlock()
	got := s.Snapshot()
	if len(got.Items) != 2 || got.Items[0].Kind != "assistant" || got.Items[1].Kind != "user" || got.Items[1].Text != notice {
		t.Fatal(got.Items)
	}
}

func TestSystemNoticeCannotEnterReplacementConversation(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	message := api.SystemMessage{ID: "original-command-result", Source: api.SystemCommandResult, Text: "Original command completed.", RuntimeOwner: "retired-native-owner"}
	receipt, err := s.SubmitSystem(t.Context(), message)
	if err != nil || receipt.ID != message.ID || receipt.Outcome != "rejected" || !receipt.Deferred {
		t.Fatal("native target fence lost", receipt, err)
	}
	if s.SystemReceipt(message.ID).Outcome != "unknown" {
		t.Fatal("unattempted input gained a native receipt")
	}
}

func TestImportedHostProvenanceIsNotDeliveryIntent(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	message := api.SystemMessage{ID: "legacy-pending-report", Source: api.SystemWorkerResult, Text: "Task completed."}
	if err := s.ImportHostReportIDs([]string{message.ID}); err != nil {
		t.Fatal(err)
	}
	if s.SystemReceipt(message.ID).Outcome != "unknown" {
		t.Fatal("provenance became acceptance")
	}
	if receipt, err := s.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("legacy pending notice was starved by its presentation marker", receipt, err)
	}
}
