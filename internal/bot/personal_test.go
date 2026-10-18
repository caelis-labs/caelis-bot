package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botmemory"
)

func TestPrivateMemoryPreservedButUnavailableToModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bot.json")
	first, err := NewForRuntime(path, "codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(dir, "personal")
	store, err := botmemory.Open(t.Context(), storePath, first.State().ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.ConfigurePersonal(store); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Remember(t.Context(), "existing-memory-1", "synthetic-private-evidence", "codex"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := NewForRuntime(path, "caelis", nil)
	if err != nil || second.State().PersonalVersion != 1 {
		t.Fatal("existing personal capability lost", err)
	}
	store, err = botmemory.Open(t.Context(), storePath, second.State().ID)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = second.ConfigurePersonal(store); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadMemory(t.Context(), "synthetic-private-evidence")
	if err != nil || len(got.Evidence) != 1 {
		t.Fatal("private memory data was removed", err)
	}
	bridge, err := Serve(first)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	config := bridge.Config("synthetic")
	for _, defs := range [][]api.ToolDefinition{first.Definitions(), first.LegacyDefinitions()} {
		for _, def := range defs {
			if def.Name == "bot_memory" {
				t.Fatal("memory remains model-visible")
			}
		}
	}
	for _, name := range config.ApprovedTools {
		if name == "bot_memory" {
			t.Fatal("memory still preapproved")
		}
	}
	for _, service := range config.Services {
		for _, name := range service.Tools {
			if name == "bot_memory" {
				t.Fatal("memory still belongs to MCP service")
			}
		}
	}
	if strings.Contains(config.Env["CAELIS_BOT_TOOL_CATALOG"], "bot_memory") {
		t.Fatal("bridge still injects memory definition")
	}
	args := json.RawMessage(`{"operation":"recall","query":"synthetic-private-evidence"}`)
	for _, name := range []string{"bot_memory", "bot_notebook"} {
		if out := first.CallTool(t.Context(), name, args); !out.IsError {
			t.Fatal("compact or hidden alias admitted", name)
		}
		if out := first.callLegacyTool(t.Context(), name, args); !out.IsError {
			t.Fatal("legacy alias admitted", name)
		}
	}
	for _, version := range []int{1, 2} {
		out := forward(config.Env["CAELIS_BOT_ENDPOINT"], toolRequest{Token: config.Env["CAELIS_BOT_TOKEN"], Version: version, Name: "bot_memory", Arguments: args})
		if !out.IsError {
			t.Fatal("bridge admitted old tool", version)
		}
	}
	if strings.Contains(config.Instructions, "synthetic-private-evidence") || strings.Contains(config.WorkerInstructions, "synthetic-private-evidence") {
		t.Fatal("private evidence leaked into instructions")
	}
}

func TestPersonalIdentityNeverDefaultsMissingOrFutureState(t *testing.T) {
	for _, raw := range []string{`{"version":1,"schedules":[]}`, `{"version":1,"id":"stable","personalVersion":2,"schedules":[]}`} {
		path := filepath.Join(t.TempDir(), "bot.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewForRuntime(path, "codex", nil); err == nil {
			t.Fatal("invalid identity accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != raw {
			t.Fatal("invalid identity rewritten")
		}
	}
}
