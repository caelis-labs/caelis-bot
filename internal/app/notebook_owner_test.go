package app

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnedResidentIncludesDormantProductionNotebookController(t *testing.T) {
	root := t.TempDir()
	if err := localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/fixture/codex"}); err != nil {
		t.Fatal(err)
	}
	a, err := NewOwnedResident(context.Background(), root, Host{}, "fixture-owner", "/fixture/caelis-node")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	prefs, err := a.Backend.NotebookSyncSettings()
	if err != nil || prefs.Enabled || prefs.IntervalMinutes != 5 {
		t.Fatal("ordinary headless owner lacks production Notebook settings", prefs, err)
	}
	state, err := a.Backend.NotebookSyncState()
	if err != nil || state.SourceNodeID != "local" || len(state.Targets) != 0 || a.started {
		t.Fatal(state, err)
	}
}

func TestOwnedResidentNormalStopAfterConfirmedNotebookSwitch(t *testing.T) {
	a := &Application{sourceRetired: true, notebookRestartPrepared: true}
	if err := a.StopNotebookOwner(t.Context()); err != nil {
		t.Fatal("already fenced owner could not close normally", err)
	}
	a.notebookRestartPrepared = false
	if err := a.StopNotebookOwner(t.Context()); err == nil {
		t.Fatal("unconfirmed switch granted stopped proof")
	}
}

func TestNotebookOwnerAcceptsOrdinaryLargeScheduleState(t *testing.T) {
	profile := t.TempDir()
	path := filepath.Join(profile, "bot.json")
	state := bot.State{Version: 1, ID: "stable-owner", Schedules: []bot.Schedule{}}
	for i := 0; i < 150; i++ {
		state.Schedules = append(state.Schedules, bot.Schedule{ID: "schedule", Label: "Synthetic schedule", Prompt: strings.Repeat("a", 1024)})
	}
	if err := localstate.Write(path, state); err != nil {
		t.Fatal(err)
	}
	var got bot.State
	if err := ReadNotebookOwnerRecord(path, &got); err != nil || got.ID != state.ID || len(got.Schedules) != 150 {
		t.Fatal("ordinary persisted schedules refused", err)
	}
	var small notebookLocalReturn
	if err := ReadNotebookOwnerRecord(path, &small); err == nil {
		t.Fatal("metadata limit was removed")
	}
}
