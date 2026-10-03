package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	"path/filepath"
	"testing"
)

func TestCompactCatalogRestoresOriginalLegacyBinding(t *testing.T) {
	root := t.TempDir()
	r, e := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.ConfigureDesktopControl(&reviewDesktopFixture{})
	old := New(Options{Directory: filepath.Join(root, "binding")})
	original := r.LegacyToolConnection()
	if e = old.ConfigureBotTools(original); e != nil {
		t.Fatal(e)
	}
	version := old.profile.ToolsVersion
	if len(old.profile.Tools) != 22 {
		t.Fatal("historical fixture changed")
	}
	restored := New(Options{Directory: filepath.Join(root, "binding")})
	if e = restored.ConfigureBotTools(&api.ToolConnection{Host: r, ApprovedTools: append(botpolicy.ApprovedTools(), desktopcontrol.ApprovedTools()...)}); e != nil {
		t.Fatal(e)
	}
	if len(restored.profile.Tools) != 10 || restored.profile.ToolsVersion == version {
		t.Fatal("new model catalog not replaced")
	}
	for _, d := range restored.profile.Tools {
		if d.Name == "bot_clock" || d.Name == "bot_task_start" {
			t.Fatal("legacy tool advertised")
		}
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
