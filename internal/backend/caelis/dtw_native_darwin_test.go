package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Opt-in native Core acceptance uses a private Host and synthetic model. The
// signed DevBot binary and installed rc.8 package are supplied by the caller.
func TestNativeDesktopWorldPluginDirectory(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	bot := os.Getenv("CAELIS_BOT_TEST_BOT_BINARY")
	packageRoot := os.Getenv("CAELIS_BOT_TEST_DTW_ROOT")
	pluginStore := os.Getenv("CAELIS_BOT_TEST_DTW_STORE")
	if bin == "" || bot == "" || packageRoot == "" || pluginStore == "" {
		t.Skip("set private Core, DevBot, Desktop World package and plugin store paths")
	}
	p, err := plugins.Load(packageRoot)
	if err != nil || p.Manifest.Name != "desktop-world" || p.Manifest.Version != "0.1.0-rc.8" || len(p.Skills) != 1 || len(p.Servers) != 1 || p.Servers[0].Type != "stdio" {
		t.Fatal("invalid installed rc.8 package", err)
	}
	var saved struct {
		Installed map[string]struct {
			Root    string
			Enabled bool
		}
	}
	state, err := os.ReadFile(filepath.Join(pluginStore, "state.json"))
	if err != nil || json.Unmarshal(state, &saved) != nil || !saved.Installed["desktop-world"].Enabled || filepath.Base(packageRoot) != saved.Installed["desktop-world"].Root {
		t.Fatal("supplied rc.8 package is not the enabled generation", err)
	}
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
	command.Dir, command.Env = root, newExecutionFixture(t, root).env
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Signal(os.Interrupt); _ = command.Wait() }()
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
	var status wire.StatusSnapshot
	if err := client.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "configure-dtw-private-model"
	var result wire.CommandResult
	if err := client.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "openai", Model: "gpt-5.4-mini", BaseUrl: pointer(provider.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		t.Fatal("configure private model", result.Outcome, err)
	}
	selection := plugins.Selection{Revision: 1, SkillRoots: p.Skills, Servers: []plugins.SelectedServer{{PackageID: "desktop-world", Name: p.Servers[0].Name, Root: p.Root, Data: filepath.Join(pluginStore, "data", "desktop-world"), Server: p.Servers[0]}}}
	session := New(Options{RequireApproval: true, Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: "openai/gpt-5.4-mini", Effort: "low", ApprovalMode: "workspace-write"}})
	if err := session.ConfigureBotTools(&api.ToolConnection{Command: bot, Host: noTools{}, Plugins: selection}); err != nil {
		t.Fatal(err)
	}
	if err := session.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	waitAcceptance(t, ctx, func() bool { return session.Snapshot().CanSend })
	submitAcceptance(t, ctx, session, "CASE_DTW_READY")
	name := plugins.RuntimeName("desktop-world", p.Servers[0].Name)
	var detail plugins.ServerDetail
	waitAcceptance(t, ctx, func() bool {
		detail, err = session.BotPluginServer(ctx, name)
		return err == nil && detail.State == "connected" && len(detail.Tools) == 1
	})
	if detail.Tools[0].Name != "exec" {
		t.Fatal("rc.8 must expose only exec", detail.Tools)
	}
	var directory wire.ApplicationMCPStatus
	sid := session.ConversationState().Session
	if err := session.client.json(ctx, "GET", "/application/sessions/"+idPath(sid)+"/mcp-status", nil, &directory, "", ""); err != nil {
		t.Fatal(err)
	}
	ready := false
	for _, skill := range directory.Skills {
		if skill.Path == p.Skills[0] && skill.Status == "ready" {
			ready = true
		}
	}
	if !ready {
		t.Fatal("rc.8 Skill missing from native Core", directory.Skills)
	}
	if err := session.UpdateBotPlugins(ctx, plugins.Selection{Revision: 2}); err != nil {
		t.Fatal(err)
	}
	detail, err = session.BotPluginServer(ctx, name)
	if err != nil || detail.State != "not_configured" || len(detail.Tools) != 0 {
		t.Fatal("disabled rc.8 tool remained in Core", detail, err)
	}
}
