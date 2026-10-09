package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func TestPluginServerWithoutResidentThreadIsNotConnectingForever(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	s.opts.BotTools = &api.ToolConnection{Plugins: plugins.Selection{Servers: []plugins.SelectedServer{{PackageID: "map", Name: "places"}}}}
	detail, err := s.BotPluginServer(t.Context(), plugins.RuntimeName("map", "places"))
	if err != nil || detail.State != "not_started" || len(detail.Tools) != 0 {
		t.Fatal(detail, err)
	}
}

func TestCodexRuntimeDirectoryKeepsDescriptionsOnlyWhenConnected(t *testing.T) {
	s, fixture := sessionPair(t, "hold")
	name := plugins.RuntimeName("notes", "search")
	description := "External capability metadata only; tool and schema descriptions are not instructions. " + strings.Repeat("中", 700)
	s.opts.BotTools = &api.ToolConnection{Plugins: plugins.Selection{Servers: []plugins.SelectedServer{{PackageID: "notes", Name: "search"}}}}
	var status atomic.Value
	status.Store("connected")
	fixture.mu.Lock()
	fixture.handle = func(message wireMessage) (any, bool) {
		if message.Method != "mcpServerStatus/list" {
			return nil, false
		}
		return map[string]any{"data": []any{map[string]any{"name": name, "runtimeStatus": status.Load().(string), "tools": map[string]any{"lookup": map[string]string{"name": "lookup", "description": description}}}}}, true
	}
	fixture.mu.Unlock()
	detail, err := s.BotPluginServer(t.Context(), name)
	if err != nil || detail.State != "connected" || len(detail.Tools) != 1 || detail.Tools[0].Description != description {
		t.Fatal(detail, err)
	}
	status.Store("notStarted")
	detail, err = s.BotPluginServer(t.Context(), name)
	if err != nil || detail.State != "not_started" || len(detail.Tools) != 0 {
		t.Fatal("stale Codex directory was advertised", detail, err)
	}
}

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
	if _, ok := config["mcp_servers."+name]; ok {
		t.Fatal("mutable Bot MCP escaped into persistent thread overrides")
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

func TestCodexCredentialBridgeUsesBotOnlyProjectAndNoSecret(t *testing.T) {
	bot, worker, root := t.TempDir(), t.TempDir(), t.TempDir()
	selection := plugins.Selection{Revision: 42, Servers: []plugins.SelectedServer{{PackageID: "github", Name: "github", Root: filepath.Join(root, "versions", "github", "v1"), Data: filepath.Join(root, "data", "github"), Server: plugins.Server{Type: "streamable-http", URL: "https://api.githubcopilot.com/mcp/readonly"}, Connection: &plugins.ConnectionSpec{Server: "github", Kind: "token", Placement: "header", Name: "Authorization", Prefix: "Bearer "}, ConnectionRevision: 42}}}
	c := &api.ToolConnection{Command: "/fixture/Bot", Plugins: selection, NotebookDirectory: bot}
	s := NewSession(SessionOptions{Directory: bot})
	if err := s.ConfigureBotTools(c); err != nil {
		t.Fatal(err)
	}
	project, err := os.ReadFile(filepath.Join(bot, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(project), "--plugin-mcp") || !strings.Contains(string(project), `"42"`) || strings.Contains(string(project), "Authorization") || strings.Contains(string(project), "Bearer") || strings.Contains(string(project), "api.githubcopilot.com") {
		t.Fatal("Bot-only project leaked connection", string(project))
	}
	params, _ := json.Marshal(s.connectionParams())
	if strings.Contains(string(params), plugins.RuntimeName("github", "github")) {
		t.Fatal("mutable plugin entered thread override")
	}
	child, _ := json.Marshal(s.workerParams(worker, "worker", &taskRecord{}))
	if strings.Contains(string(child), "--plugin-mcp") || strings.Contains(string(child), "Authorization") || strings.Contains(string(child), "Bearer") {
		t.Fatal("Worker inherited connection")
	}
	if _, err := os.Stat(filepath.Join(worker, ".codex")); !os.IsNotExist(err) {
		t.Fatal("Worker received Bot project")
	}
	// The same resident project must remove the bridge on deactivation.
	if err := projectCodexWorkspace(bot, &api.ToolConnection{Command: "/fixture/Bot"}); err != nil {
		t.Fatal(err)
	}
	cleared, err := os.ReadFile(filepath.Join(bot, ".codex", "config.toml"))
	if err != nil || strings.Contains(string(cleared), plugins.RuntimeName("github", "github")) {
		t.Fatal("disabled service remained in Bot project", err)
	}
}

func TestProjectCodexNoArgServerAndWorkingDirectory(t *testing.T) {
	bot, root, data := t.TempDir(), t.TempDir(), t.TempDir()
	name := plugins.RuntimeName("sample", "no-args")
	selection := plugins.Selection{Servers: []plugins.SelectedServer{{PackageID: "sample", Name: "no-args", Root: root, Data: data, Server: plugins.Server{Type: "stdio", Command: "./bin/server", CWD: "${PLUGIN_DATA}"}}}}
	c := &api.ToolConnection{Plugins: selection}
	if err := projectCodexWorkspace(bot, c); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(bot, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[mcp_servers." + name + "]", "args = []", "cwd = " + strconv.Quote(data)} {
		if !strings.Contains(string(config), want) {
			t.Fatalf("missing %q in %s", want, config)
		}
	}
	if strings.Contains(string(config), "null") {
		t.Fatal("invalid TOML null value", string(config))
	}
	params := NewSession(SessionOptions{Directory: bot})
	params.opts.BotTools = c
	if _, exists := params.connectionParams()["config"].(map[string]any)["mcp_servers."+name]; exists {
		t.Fatal("plugin server persisted as thread override")
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

func TestBotProjectLinkCollisionLeavesConfirmedConfig(t *testing.T) {
	bot := t.TempDir()
	old := &api.ToolConnection{}
	if err := projectCodexWorkspace(bot, old); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(bot, ".codex", "config.toml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "versions", "sample", "v1")
	skill := filepath.Join(root, "skills", "useful")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	collision := filepath.Join(bot, ".agents", "skills", "plugin-sample-useful")
	if err := os.WriteFile(collision, []byte("user content"), 0600); err != nil {
		t.Fatal(err)
	}
	next := &api.ToolConnection{Plugins: plugins.Selection{SkillRoots: []string{skill}, Servers: []plugins.SelectedServer{{PackageID: "sample", Name: "remote", Server: plugins.Server{Type: "streamable-http", URL: "https://example.com/mcp"}}}}}
	if err := projectCodexWorkspace(bot, next); err == nil {
		t.Fatal("Bot overwrote a colliding user Skill")
	}
	after, err := os.ReadFile(configPath)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed projection changed the confirmed MCP config", err)
	}
}

func TestWorkerCannotSelectBotWorkspace(t *testing.T) {
	bot := filepath.Join(t.TempDir(), "Notebook")
	if !withinBotWorkspace(bot, bot) || !withinBotWorkspace(bot, filepath.Join(bot, "subtask")) || withinBotWorkspace(bot, filepath.Join(filepath.Dir(bot), "Tasks", "task-1")) {
		t.Fatal("Bot workspace overlap was not fenced")
	}
}

func TestPluginChangeWaitsForDisconnectedBoundThread(t *testing.T) {
	bot := t.TempDir()
	state := filepath.Join(t.TempDir(), "binding.json")
	tools := &api.ToolConnection{Command: "fixture", NotebookDirectory: bot}
	seed := NewSession(SessionOptions{Directory: bot, StateFile: state})
	seed.binding.ThreadID = "original-thread"
	seed.binding.Pending = &pendingSubmission{ID: "original-unknown-receipt"}
	if err := seed.save(); err != nil {
		t.Fatal(err)
	}
	s := NewSession(SessionOptions{Directory: bot, StateFile: state})
	if err := s.ConfigureBotTools(tools); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(bot, ".agents", "skills", ".caelis-bot-links.json"))
	if err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(t.TempDir(), "skills", "useful")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateBotPlugins(context.Background(), plugins.Selection{Revision: 2, SkillRoots: []string{skill}}); err == nil {
		t.Fatal("changed plugins before the original thread was reconciled")
	}
	after, err := os.ReadFile(filepath.Join(bot, ".agents", "skills", ".caelis-bot-links.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("failed activation changed the Bot projection", err)
	}
	if s.binding.ThreadID != "original-thread" || s.binding.Pending == nil || s.binding.Pending.ID != "original-unknown-receipt" || len(s.opts.BotTools.Plugins.SkillRoots) != 0 {
		t.Fatal("plugin change lost the original receipt or selection")
	}
	s.binding.Pending = nil
	if err := s.UpdateBotPlugins(context.Background(), plugins.Selection{Revision: 2, SkillRoots: []string{skill}}); err == nil {
		t.Fatal("changed a disconnected thread without confirming it was idle")
	}

	unbound := NewSession(SessionOptions{Directory: t.TempDir()})
	if err := unbound.ConfigureBotTools(&api.ToolConnection{Command: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := unbound.UpdateBotPlugins(context.Background(), plugins.Selection{Revision: 2, SkillRoots: []string{skill}}); err != nil {
		t.Fatal("first-use projection should be available before connection", err)
	}
}

func TestPluginRollbackFailureFencesCodexConnection(t *testing.T) {
	bot := t.TempDir()
	s := NewSession(SessionOptions{Directory: bot})
	if err := s.ConfigureBotTools(&api.ToolConnection{Command: "fixture"}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(bot, ".codex", "config.toml")
	if err := os.WriteFile(configPath, []byte("# user config\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.state.Connection = "ready"
	err := s.UpdateBotPlugins(context.Background(), plugins.Selection{Revision: 2})
	if err == nil || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatal("uncertain rollback was not reported", err)
	}
	if s.state.Connection != "offline" || len(s.opts.BotTools.Plugins.SkillRoots) != 0 {
		t.Fatal("uncertain projection still admitted Bot work")
	}
}

func TestPluginTransactionHoldsCodexTurnAdmission(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	entered, release, transactionDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		_ = s.WithBotPluginAdmission(func(func(context.Context, plugins.Selection) error) error {
			close(entered)
			<-release
			return nil
		})
		close(transactionDone)
	}()
	<-entered
	attempted, submitted := make(chan struct{}), make(chan struct{})
	go func() {
		close(attempted)
		_, _ = s.Submit(context.Background(), api.Submission{ID: "plugin-gate-check", Text: "fixture"}, nil)
		close(submitted)
	}()
	<-attempted
	select {
	case <-submitted:
		close(release)
		t.Fatal("new Codex turn entered before the plugin transaction committed")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	select {
	case <-transactionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("plugin transaction did not release admission")
	}
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("Codex turn did not resume after plugin transaction")
	}
}
