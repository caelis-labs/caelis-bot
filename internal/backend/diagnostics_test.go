package backend

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestDiagnosticsUsesAllowlistNotPrivateSnapshotContent(t *testing.T) {
	const secret = "PRIVATE_SENTINEL"
	e := snapshotEngine{value: api.Snapshot{Connection: "ready", ConnectionIssue: secret, Phase: secret,
		Message: secret, CurrentTurn: secret, LastReceipt: api.Receipt{ID: secret, Message: secret},
		Items:     []api.Item{{ID: secret, Text: secret, Details: secret, Artifacts: []api.Artifact{{ID: secret, Name: secret}}}},
		Approvals: []api.Approval{{ID: secret, Action: secret, Target: secret}}, References: []api.Reference{{Name: secret, Description: secret}}}}
	s := NewService(e, nil, nil, nil, nil)
	s.runtimeSettings.CLIPath = "/private/" + secret
	s.draft.Text = secret
	b, err := s.DiagnosticReport()
	if err != nil || !json.Valid(b) || strings.Contains(string(b), secret) || strings.Contains(string(b), "/private/") {
		t.Fatal("private content escaped diagnostic boundary")
	}
	var report map[string]any
	_ = json.Unmarshal(b, &report)
	if report["connection"] != "ready" || report["phase"] != "other" || report["draftPresent"] != true {
		t.Fatal("useful allowlisted status missing")
	}
}

type revisionEngine struct {
	snapshotEngine
	revision uint64
	calls    int
}

func (e *revisionEngine) Revision() uint64       { return e.revision }
func (e *revisionEngine) Snapshot() api.Snapshot { e.calls++; return e.value }
func TestUnchangedChatPollDoesNotSerializeHistory(t *testing.T) {
	e := &revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Items: []api.Item{{Kind: "user", Text: "hello"}, {Kind: "activity", Details: "tool output"}}}}}
	s := NewService(e, nil, nil, nil, nil)
	first := s.ChatSnapshot(0, "")
	if !first.Changed || len(first.Snapshot.Items) != 1 {
		t.Fatal("chat projection includes tools")
	}
	if s.ChatSnapshot(7, "").Changed || e.calls != 1 {
		t.Fatal("unchanged poll fetched all history")
	}
	s.SetBotStatus(func() string { return "提醒正在排队" })
	update := s.ChatSnapshot(7, "")
	if !update.Changed || update.Snapshot.Message != "提醒正在排队" || s.ChatSnapshot(7, "提醒正在排队").Changed {
		t.Fatal("schedule status changes were hidden by history polling")
	}
}
