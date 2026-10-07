package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func TestBotProjectProjectionAndWorkerIsolation(t *testing.T) {
	bot := t.TempDir()
	worker := t.TempDir()
	root := t.TempDir()
	skill := filepath.Join(root, "skills", "useful")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	selection := plugins.Selection{SkillRoots: []string{skill}, Servers: []plugins.SelectedServer{{PackageID: "sample", Name: "local-validator", Root: root, Data: filepath.Join(t.TempDir(), "data"), Server: plugins.Server{Type: "stdio", Command: "./bin/check", Args: []string{"${PLUGIN_ROOT}/rules.json"}, Env: map[string]string{"RULES": "${PLUGIN_ROOT}/rules.json"}}}}}
	c := &api.ToolConnection{Command: "fixture", Env: map[string]string{"CAELIS_BOT_TOOL_CATALOG": "[]"}, NotebookDirectory: bot, Plugins: selection}
	s := NewSession(SessionOptions{Directory: bot})
	if err := s.ConfigureBotTools(c); err != nil {
		t.Fatal(err)
	}
	config := s.connectionParams()["config"].(map[string]any)
	name := plugins.RuntimeName("sample", "local-validator")
	if _, ok := config["mcp_servers."+name]; !ok {
		t.Fatal("Bot MCP missing")
	}
	project, err := os.ReadFile(filepath.Join(bot, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(project), name) || !strings.Contains(string(project), "PLUGIN_ROOT") {
		t.Fatal("portable env missing from Bot-only project")
	}
	links, err := os.ReadDir(filepath.Join(bot, ".agents", "skills"))
	if err != nil || len(links) != 2 {
		t.Fatal("Skill projection", links, err)
	}
	child := s.workerParams(worker, "worker", &taskRecord{})
	raw, _ := json.Marshal(child)
	if strings.Contains(string(raw), bot) || strings.Contains(string(raw), "PLUGIN_ROOT") {
		t.Fatal("Worker inherited Bot plugin", string(raw))
	}
	blocked, ok := child["config"].(map[string]any)["mcp_servers."+name].(map[string]any)
	if !ok || blocked["enabled"] != false {
		t.Fatal("Worker did not disable resident plugin service")
	}
	if _, err := os.Stat(filepath.Join(worker, ".codex")); !os.IsNotExist(err) {
		t.Fatal("Worker received project config")
	}
	if _, err := os.Stat(filepath.Join(worker, ".agents")); !os.IsNotExist(err) {
		t.Fatal("Worker received Skills")
	}
}

func TestBotProjectRejectsEscapingLinksAndSymlinkDirectories(t *testing.T) {
	bot := t.TempDir()
	skillDir := filepath.Join(bot, ".agents", "skills")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(bot, ".agents", "escape")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".caelis-bot-links.json"), []byte(`{"../escape":"/tmp/target"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := projectCodexWorkspace(bot, &api.ToolConnection{}); err == nil {
		t.Fatal("escaping manifest accepted")
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "keep" {
		t.Fatal("outside path changed", err)
	}
	if _, err := os.Stat(filepath.Join(bot, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatal("project config changed after unsafe manifest")
	}
	other := t.TempDir()
	alias := filepath.Join(other, ".agents")
	if err := os.Symlink(skillDir, alias); err != nil {
		t.Fatal(err)
	}
	if err := projectCodexWorkspace(other, &api.ToolConnection{}); err == nil {
		t.Fatal("symlink project directory accepted")
	}
}

func TestWorkerCannotSelectBotWorkspace(t *testing.T) {
	bot := filepath.Join(t.TempDir(), "Notebook")
	if !withinBotWorkspace(bot, bot) || !withinBotWorkspace(bot, filepath.Join(bot, "subtask")) || withinBotWorkspace(bot, filepath.Join(filepath.Dir(bot), "Tasks", "task-1")) {
		t.Fatal("Bot workspace overlap was not fenced")
	}
}
