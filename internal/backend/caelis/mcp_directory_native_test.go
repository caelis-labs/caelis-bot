package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Opt-in acceptance against a separately built Core Host and local synthetic
// MCP service. The model endpoint below is an in-process deterministic fixture.
func TestNativeMCPDirectoryMetadata(t *testing.T) {
	bin, mcpURL := os.Getenv("CAELIS_BOT_TEST_BINARY"), os.Getenv("CAELIS_BOT_TEST_MCP_URL")
	if bin == "" || mcpURL == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY and CAELIS_BOT_TEST_MCP_URL for isolated native MCP acceptance")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model := newAcceptanceModel()
	modelServer := httptest.NewServer(http.HandlerFunc(model.serve))
	defer modelServer.Close()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	environment := newExecutionFixture(t, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, environment.env
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
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
	var hostStatus wire.StatusSnapshot
	if err := client.json(ctx, "GET", "/status", nil, &hostStatus, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "configure-native-mcp-fixture"
	var result wire.CommandResult
	if err := client.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &hostStatus.Configuration.Revision, Config: wire.ConnectConfig{Provider: "openai", Model: "gpt-5.4-mini", BaseUrl: pointer(modelServer.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(hostStatus.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		t.Fatalf("configure synthetic model: %+v %v", result, err)
	}
	selection := plugins.Selection{Revision: 1, Servers: []plugins.SelectedServer{{PackageID: "fixture", Name: "catalog", Server: plugins.Server{Type: "streamable-http", URL: mcpURL}}}}
	session := New(Options{RequireApproval: true, Directory: filepath.Join(root, "bot"), Settings: settings, Execution: api.ExecutionSettings{Model: "openai/gpt-5.4-mini", Effort: "low", ApprovalMode: "workspace-write"}})
	if err := session.ConfigureBotTools(&api.ToolConnection{Host: noTools{}, Plugins: selection}); err != nil {
		t.Fatal(err)
	}
	if err := session.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	waitAcceptance(t, ctx, func() bool { return session.Snapshot().CanSend })
	name := plugins.RuntimeName("fixture", "catalog")
	before, err := session.BotPluginServer(ctx, name)
	if err != nil || before.State != "not_started" || len(before.Tools) != 0 {
		t.Fatalf("pre-turn Runtime directory: %+v %v", before, err)
	}
	submitAcceptance(t, ctx, session, "CASE_MCP_READY")
	var detail plugins.ServerDetail
	waitAcceptance(t, ctx, func() bool {
		detail, err = session.BotPluginServer(ctx, name)
		return err == nil && detail.State == "connected" && len(detail.Tools) == 1200
	})
	if len(model.seen("CASE_MCP_READY")) == 0 || detail.Tools[0].Name != "lookup_0000" || detail.Tools[1199].Name != "lookup_1199" || !strings.Contains(detail.Tools[1199].Description, "Synthetic lookup 1199") || len(detail.Tools[1199].Description) <= 128 {
		t.Fatalf("incomplete native Runtime metadata: count=%d first=%+v last=%+v", len(detail.Tools), detail.Tools[0], detail.Tools[1199])
	}
	listCount := -1
	if audit := os.Getenv("CAELIS_BOT_TEST_MCP_AUDIT"); audit != "" {
		b, err := os.ReadFile(audit)
		if err != nil {
			t.Fatal(err)
		}
		listCount = strings.Count(string(b), "tools/list\n")
		if listCount == 0 {
			t.Fatal("Core did not list synthetic MCP tools")
		}
	}
	indexPath := filepath.Join(root, "app-skills", "mcp-tools.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0700); err != nil {
		t.Fatal(err)
	}
	connected := []plugins.IndexServer{{PackageID: "fixture", Name: "catalog", RuntimeName: name, Tools: detail.Tools}}
	if err := plugins.WriteIndex(indexPath, connected); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Services []struct {
			Tools []struct {
				Name, Description string
			}
		}
	}
	content, err := os.ReadFile(indexPath)
	if err != nil || json.Unmarshal(content, &index) != nil || len(index.Services) != 1 || len(index.Services[0].Tools) != 1200 || index.Services[0].Tools[1199].Description == "" {
		t.Fatalf("native metadata index incomplete: bytes=%d err=%v", len(content), err)
	}
	for i, tool := range index.Services[0].Tools {
		if tool.Name != detail.Tools[i].Name || tool.Description != detail.Tools[i].Description || len(tool.Description) <= 128 {
			t.Fatalf("native metadata mismatch at tool %d: index=%+v Runtime=%+v", i, tool, detail.Tools[i])
		}
	}
	for range 3 {
		current, err := session.BotPluginServer(ctx, name)
		if err != nil || current.State != "connected" || len(current.Tools) != 1200 {
			t.Fatalf("status refresh lost ready tools: %+v %v", current, err)
		}
	}
	if err := plugins.WriteIndex(indexPath, connected); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(indexPath)
	if err != nil || !os.SameFile(first, second) || !first.ModTime().Equal(second.ModTime()) {
		t.Fatalf("unchanged index was rewritten: %v", err)
	}
	if audit := os.Getenv("CAELIS_BOT_TEST_MCP_AUDIT"); audit != "" {
		b, err := os.ReadFile(audit)
		if err != nil || strings.Count(string(b), "tools/list\n") != listCount {
			t.Fatalf("status/index refresh made another MCP tools/list call: before=%d after=%d err=%v", listCount, strings.Count(string(b), "tools/list\n"), err)
		}
	}
	if err := session.UpdateBotPlugins(ctx, plugins.Selection{Revision: 2}); err != nil {
		t.Fatal(err)
	}
	retired, err := session.BotPluginServer(ctx, name)
	if err != nil || retired.State != "not_configured" || len(retired.Tools) != 0 {
		t.Fatalf("disabled Runtime retained tools: %+v %v", retired, err)
	}
	if err := plugins.WriteIndex(indexPath, nil); err != nil {
		t.Fatal(err)
	}
	broken := plugins.Selection{Revision: 3, Servers: []plugins.SelectedServer{{PackageID: "fixture", Name: "catalog", Server: plugins.Server{Type: "streamable-http", URL: "http://127.0.0.1:1/mcp"}}}}
	if err := session.UpdateBotPlugins(ctx, broken); err != nil {
		t.Fatal(err)
	}
	submitAcceptance(t, ctx, session, "CASE_MCP_DISCONNECTED")
	disconnected, err := session.BotPluginServer(ctx, name)
	if err != nil || disconnected.State == "connected" || len(disconnected.Tools) != 0 {
		t.Fatalf("unreachable MCP retained ready directory: %+v %v", disconnected, err)
	}
	reconnected := selection.Clone()
	reconnected.Revision = 4
	if err := session.UpdateBotPlugins(ctx, reconnected); err != nil {
		t.Fatal(err)
	}
	submitAcceptance(t, ctx, session, "CASE_MCP_RECONNECTED")
	recovered, err := session.BotPluginServer(ctx, name)
	if err != nil || recovered.State != "connected" || len(recovered.Tools) != 1200 || recovered.Tools[1199].Description != detail.Tools[1199].Description {
		t.Fatalf("reconfigured MCP directory did not recover: %+v %v", recovered.State, err)
	}
	oldGeneration := session.BotPluginGeneration()
	fresh := New(Options{RequireApproval: true, Directory: filepath.Join(root, "bot-fresh"), Settings: settings, Execution: api.ExecutionSettings{Model: "openai/gpt-5.4-mini", Effort: "low", ApprovalMode: "workspace-write"}})
	if err := fresh.ConfigureBotTools(&api.ToolConnection{Host: noTools{}, Plugins: reconnected}); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fresh.Close(context.Background()) }()
	waitAcceptance(t, ctx, func() bool { return fresh.Snapshot().CanSend })
	freshBefore, err := fresh.BotPluginServer(ctx, name)
	if err != nil || freshBefore.State != "not_started" || len(freshBefore.Tools) != 0 || fresh.BotPluginGeneration() == oldGeneration {
		t.Fatalf("new Core Session inherited the old MCP directory: %+v %v", freshBefore, err)
	}
	submitAcceptance(t, ctx, fresh, "CASE_MCP_FRESH")
	freshReady, err := fresh.BotPluginServer(ctx, name)
	if err != nil || freshReady.State != "connected" || len(freshReady.Tools) != 1200 {
		t.Fatalf("new Core Session did not refresh MCP directory: %+v %v", freshReady.State, err)
	}
	t.Logf("synthetic receipts: model=%s/%s, turns=case_mcp_ready,case_mcp_disconnected,case_mcp_reconnected,case_mcp_fresh, sessions=%s/%s", op, result.Outcome, session.ConversationState().Session, fresh.ConversationState().Session)
	t.Logf("synthetic model requests=%d, ready tools=%d, index bytes=%d, initial tools/list=%d, states=%s/%s/%s/%s/%s, new generation=true", len(model.seen("CASE_MCP_READY"))+len(model.seen("CASE_MCP_DISCONNECTED"))+len(model.seen("CASE_MCP_RECONNECTED"))+len(model.seen("CASE_MCP_FRESH")), len(detail.Tools), len(content), listCount, before.State, retired.State, disconnected.State, recovered.State, freshBefore.State)
}
