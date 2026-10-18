package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// Opt-in Windows native Core + packaged MCP acceptance uses a private Host
// and synthetic model. It does not perform a desktop action or read credentials.
func TestWindowsNativeDesktopWorldPluginDirectory(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	bot := os.Getenv("CAELIS_BOT_TEST_BOT_BINARY")
	packageRoot := os.Getenv("CAELIS_BOT_TEST_DTW_ROOT")
	pluginStore := os.Getenv("CAELIS_BOT_TEST_DTW_STORE")
	if bin == "" || bot == "" || packageRoot == "" || pluginStore == "" {
		t.Skip("set private Core, DevBot, Desktop World package and plugin store paths")
	}
	p, err := plugins.Load(packageRoot)
	if err != nil || p.Manifest.Name != "desktop-world" || p.Manifest.Version != "0.1.0-rc.9" || len(p.Skills) != 1 || len(p.Servers) != 1 || p.Servers[0].Type != "stdio" {
		t.Fatalf("invalid installed rc.9 package: %v; skills=%d servers=%d issues=%v", err, len(p.Skills), len(p.Servers), p.Issues)
	}
	var saved struct {
		Installed map[string]struct {
			Root    string
			Enabled bool
		}
	}
	state, err := os.ReadFile(filepath.Join(pluginStore, "state.json"))
	if err != nil || json.Unmarshal(state, &saved) != nil || !saved.Installed["desktop-world"].Enabled || filepath.Base(packageRoot) != saved.Installed["desktop-world"].Root {
		t.Fatal("supplied rc.9 package is not the enabled generation", err)
	}
	// Run this opt-in test executable beside the same verified Plugins payload.
	// A first-run settings window has not acquired the resident owner's lease.
	manager, err := plugins.Open(pluginStore)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.AcquireDesktopOwner(); err != nil {
		t.Fatal("private Bot owner", err)
	}
	defer manager.CloseDesktopOwner()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	model := newAcceptanceModel()
	provider := httptest.NewServer(http.HandlerFunc(model.serve))
	defer provider.Close()
	command := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	command.Dir, command.Env = root, runtimeenv.Clean(os.Environ())
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	waitAcceptance(t, ctx, func() bool { _, _, err := Discover(settings); return err == nil })
	discovery, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newClient(discovery.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initialize(ctx, client); err != nil {
		t.Fatal(err)
	}
	// v0.71.0 uses the native custom provider for a Responses endpoint. Keep
	// the selected profile's exact catalog value rather than a provider/name guess.
	if err := ApplySetup(ctx, api.SetupRequest{Settings: settings, Action: "connect-model", Provider: "custom", Model: "gpt-5.4-mini", BaseURL: provider.URL + "/v1", APIKey: "SYNTHETIC_ONLY"}); err != nil {
		t.Fatal("configure private model", err)
	}
	modelSelector := fixtureModelSelector(t, ctx, client, "gpt-5.4-mini")
	selection := plugins.Selection{Revision: 1, SkillRoots: p.Skills, Servers: []plugins.SelectedServer{{PackageID: "desktop-world", Name: p.Servers[0].Name, Root: p.Root, Data: filepath.Join(pluginStore, "data", "desktop-world"), Server: p.Servers[0]}}}
	session := New(Options{RequireApproval: true, Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: modelSelector, ApprovalMode: "workspace-write"}})
	if err := session.ConfigureBotTools(&api.ToolConnection{Command: bot, Host: noTools{}, NotebookDirectory: root, Plugins: selection}); err != nil {
		t.Fatal(err)
	}
	if err := session.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	t.Logf("native connection phase=%s message=%s issue=%s", session.Snapshot().Phase, session.Snapshot().Message, session.Snapshot().ConnectionIssue)
	waitAcceptance(t, ctx, func() bool { return session.Snapshot().CanSend })
	submitAcceptance(t, ctx, session, "CASE_DTW_READY")
	name := plugins.RuntimeName("desktop-world", p.Servers[0].Name)
	var detail plugins.ServerDetail
	defer func() {
		if t.Failed() {
			t.Logf("MCP state=%s error=%s transport=%v", detail.State, detail.Error, err)
		}
	}()
	waitAcceptance(t, ctx, func() bool {
		detail, err = session.BotPluginServer(ctx, name)
		if detail.State == "failed" {
			t.Fatalf("native MCP failed: %s %v", detail.Error, err)
		}
		return err == nil && detail.State == "connected" && len(detail.Tools) == 1
	})
	if detail.Tools[0].Name != "exec" {
		t.Fatal("rc.9 must expose only exec", detail.Tools)
	}
	var directory wire.SessionMCPStatus
	sid := session.ConversationState().Session
	if err := session.client.json(ctx, "GET", "/sessions/"+idPath(sid)+"/mcp-status", nil, &directory, "", ""); err != nil {
		t.Fatal(err)
	}
	ready := false
	for _, skill := range directory.Skills {
		if skill.Path == p.Skills[0] && skill.Status == "ready" {
			ready = true
		}
	}
	if !ready {
		t.Fatal("rc.9 Skill missing from native Core", directory.Skills)
	}
	if err := session.UpdateBotPlugins(ctx, plugins.Selection{Revision: 2}); err != nil {
		t.Fatal(err)
	}
	detail, err = session.BotPluginServer(ctx, name)
	if err != nil || detail.State != "not_configured" || len(detail.Tools) != 0 {
		t.Fatal("disabled rc.9 tool remained in Core", detail, err)
	}
}
