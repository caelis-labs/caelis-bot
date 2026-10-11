package backend

import (
	"encoding/json"
	"path/filepath"
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

func TestConfiguredChatPollTracksIndependentVersionsWithoutProjectingIdleHistory(t *testing.T) {
	e := &revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Connection: "offline", Items: []api.Item{}}}}
	s := NewService(e, nil, nil, nil, nil)
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	first := s.ChatSnapshot(0, "").Snapshot
	if update := s.ChatSnapshot(first.Revision, first.BotStatus); update.Changed || e.calls != 1 {
		t.Fatalf("idle configured chat projected history: changed=%v calls=%d", update.Changed, e.calls)
	}
	// The local log can advance after the engine becomes idle (for example,
	// asynchronous startup loading or an explicit earlier-page load).
	s.chat.Observe([]api.Item{{ID: "local", Kind: "assistant", Text: "synthetic", Status: "completed"}})
	local := s.ChatSnapshot(first.Revision, first.BotStatus)
	if !local.Changed || len(local.Snapshot.Items) != 1 || local.Snapshot.Items[0].ID != "local" {
		t.Fatal("independent chatlog update was hidden")
	}
	if s.ChatSnapshot(local.Snapshot.Revision, local.Snapshot.BotStatus).Changed {
		t.Fatal("unchanged chatlog still projected")
	}
	// Runtime approval and receipt changes remain engine-owned.
	e.revision++
	e.value.Revision++
	e.value.Approvals = []api.Approval{{ID: "approval", Status: "pending"}}
	engine := s.ChatSnapshot(local.Snapshot.Revision, local.Snapshot.BotStatus)
	if !engine.Changed || len(engine.Snapshot.Approvals) != 1 {
		t.Fatal("engine approval update was hidden")
	}
	s.RequireSetup(true)
	setup := s.ChatSnapshot(engine.Snapshot.Revision, engine.Snapshot.BotStatus)
	if !setup.Changed || setup.Snapshot.ConnectionIssue != "setup_required" {
		t.Fatal("local connection presentation update was hidden")
	}
	if !s.stageOutgoing(api.Submission{ID: "original", Text: "synthetic", ScreenInput: true}, nil) {
		t.Fatal("could not stage original outgoing request")
	}
	outgoing := s.ChatSnapshot(setup.Snapshot.Revision, setup.Snapshot.BotStatus)
	if !outgoing.Changed || len(outgoing.Snapshot.Items) != 2 || outgoing.Snapshot.Items[1].RequestID != "original" {
		t.Fatal("local outgoing update was hidden", outgoing.Snapshot.Items)
	}
	if s.ChatSnapshot(outgoing.Snapshot.Revision, outgoing.Snapshot.BotStatus).Changed {
		t.Fatal("stable outgoing request still projected")
	}
	s.finishOutgoing("original", api.Receipt{ID: "original", Outcome: "unknown"})
	unknown := s.ChatSnapshot(outgoing.Snapshot.Revision, outgoing.Snapshot.BotStatus)
	if !unknown.Changed || unknown.Snapshot.Items[1].Status != "unknown" {
		t.Fatal("original unknown receipt was hidden")
	}
}

func TestChatPollDoesNotAcknowledgeLocalChangeDuringProjection(t *testing.T) {
	e := &revisionEngine{revision: 7, snapshotEngine: snapshotEngine{value: api.Snapshot{Revision: 7, Items: []api.Item{}}}}
	s := NewService(e, nil, nil, nil, nil)
	calls := 0
	s.SetBotStatus(func() string {
		calls++
		if calls == 4 {
			s.stageOutgoing(api.Submission{ID: "during-projection", Text: "synthetic", ScreenInput: true}, nil)
		}
		return ""
	})
	_ = s.ChatSnapshot(0, "")
	stale := s.ChatSnapshot(0, "").Snapshot
	if len(stale.Items) != 0 {
		t.Fatal("fixture did not stage the outgoing request after projection")
	}
	changed := s.ChatSnapshot(stale.Revision, stale.BotStatus)
	if !changed.Changed || len(changed.Snapshot.Items) != 1 || changed.Snapshot.Items[0].RequestID != "during-projection" {
		t.Fatal("concurrent local update was acknowledged with an older projection")
	}
}
