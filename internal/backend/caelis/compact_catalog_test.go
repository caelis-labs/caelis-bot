package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"path/filepath"
	"testing"
)

func TestCompactCatalogRestoresOriginalLegacyBinding(t *testing.T) {
	root := t.TempDir()
	r, e := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if e != nil {
		t.Fatal(e)
	}
	old := New(Options{Directory: filepath.Join(root, "binding")})
	original := r.LegacyToolConnection()
	if e = old.ConfigureBotTools(original); e != nil {
		t.Fatal(e)
	}
	version := old.profile.ToolsVersion
	if len(old.profile.Tools) != len(r.LegacyDefinitions()) {
		t.Fatal("historical fixture changed")
	}
	previous := New(Options{Directory: filepath.Join(root, "previous-binding")})
	historical := r.HistoricalToolConnections()[0]
	if e = previous.ConfigureBotTools(historical); e != nil {
		t.Fatal(e)
	}
	previousVersion := previous.profile.ToolsVersion
	restored := New(Options{Directory: filepath.Join(root, "binding")})
	if e = restored.ConfigureBotTools(&api.ToolConnection{Host: r, ApprovedTools: botpolicy.ApprovedTools()}); e != nil {
		t.Fatal(e)
	}
	if len(restored.profile.Tools) != len(r.Definitions()) || restored.profile.ToolsVersion == version {
		t.Fatal("new model catalog not replaced")
	}
	for _, d := range restored.profile.Tools {
		if d.Name == "bot_clock" || d.Name == "bot_task_start" || d.Name == "bot_memory" {
			t.Fatal("legacy tool advertised")
		}
	}
	dream := false
	for _, d := range restored.profile.Tools {
		dream = dream || d.Name == "bot_dream"
	}
	if !dream || restored.catalog["bot_memory"] != nil || restored.catalogs[version]["bot_memory"] != nil {
		t.Fatal("current or legacy Caelis catalog has wrong memory/Dream scope")
	}
	if previousVersion == version || restored.catalogs[previousVersion]["bot_clock"] == nil {
		t.Fatal("pre-retirement non-memory callback version was not restored")
	}
	if out := restored.catalogs[previousVersion]["bot_clock"].CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); out.IsError {
		t.Fatal("pre-retirement clock callback lost")
	}
	if out := restored.catalogs[previousVersion]["bot_memory"].CallTool(t.Context(), "bot_memory", json.RawMessage(`{"operation":"recall"}`)); !out.IsError {
		t.Fatal("pre-retirement memory callback still executed")
	}
	handler := restored.catalogs[version]["bot_clock"]
	if handler == nil {
		t.Fatal("original version handler missing")
	}
	if out := handler.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); out.IsError {
		t.Fatal("original clock receipt not recoverable")
	}
	if out := r.CallTool(t.Context(), "bot_clock", json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("new calls bypassed current catalog")
	}
	oldTasks := restored.catalogs[version]["bot_tasks"]
	newTasks := restored.catalogs[restored.profile.ToolsVersion]["bot_tasks"]
	if oldTasks == nil || newTasks == nil || oldTasks == newTasks {
		t.Fatal("same name merged original argument contract")
	}
}
