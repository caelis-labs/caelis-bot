package app

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/telegram"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedUpdateWaitsForOriginalWorkAndUnknown(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	for _, snapshot := range []api.Snapshot{
		{CanInterrupt: true}, {Phase: "sending"}, {Phase: "unknown"},
		{LastReceipt: api.Receipt{ID: "original", Outcome: "unknown"}},
		{Approvals: []api.Approval{{ID: "original-decision", Status: "pending"}}},
	} {
		e.snapshot = snapshot
		if err := a.PrepareIdleUpdate(); err == nil {
			t.Fatal("active/unknown owner admitted", snapshot)
		}
		if a.updatePrepared || e.detached != 0 || e.closed != 0 {
			t.Fatal("failed preflight changed owner")
		}
	}
	e.snapshot = api.Snapshot{}
	if err := a.PrepareIdleUpdate(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "racing-input"}); err == nil {
		t.Fatal("admission not fenced")
	}
	a.CancelUpdate()
	if a.updatePrepared {
		t.Fatal("cancel retained fence")
	}
}

func TestManagedUpdateHandsOffPausedChannelWithoutSettlingUnknown(t *testing.T) {
	e := &updateEngine{testEngine: newTestEngine()}
	a, _ := fixtureApp(t, e, Host{})
	root := t.TempDir()
	original := []byte(`{"version":1,"enabled":false,"chatId":1,"inputs":{},"messages":{"original-outbound":{"state":"unknown"}}}`)
	path := filepath.Join(root, "telegram.json")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	bridge, err := telegram.Open(root, telegram.Host{})
	if err != nil {
		t.Fatal(err)
	}
	a.Telegram = bridge
	if err = a.PrepareIdleUpdate(); err != nil {
		t.Fatal(err)
	}
	reopened, err := telegram.Open(root, telegram.Host{})
	if err != nil || reopened.Status().Enabled || !reopened.Status().Paired || !a.updatePrepared {
		t.Fatal("paused channel not handed off", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(stored), `"original-outbound"`) {
		t.Fatal("original receipt removed", err)
	}
	a.CancelUpdate()
	if bridge.Status().Enabled {
		t.Fatal("update enabled a paused channel")
	}
}
