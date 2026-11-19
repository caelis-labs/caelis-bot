package caelis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func TestBotProfileLeavesProjectMCPToNativeDiscovery(t *testing.T) {
	notebook := t.TempDir()
	if err := os.Mkdir(filepath.Join(notebook, "tools"), 0700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceMCP(t, notebook, `{
  "mcpServers": {
    "local": {"command": "node", "args": ["server.js"], "workDir": "tools"},
    "remote": {"url": "https://mcp.example.test/"}
  }
}`)

	selection := plugins.Selection{Revision: 5, Servers: []plugins.SelectedServer{{
		PackageID: "reviewed",
		Name:      "search",
		Server:    plugins.Server{Type: "streamable-http", URL: "https://plugin.example.test/mcp"},
	}}}
	var revision wire.Uint64Decimal = "1"
	var requested wire.SessionConfigurationPatch
	var current wire.SessionProfile
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeFixture(w, wire.SessionConfiguration{SessionId: "main", Revision: revision, Profile: &current})
		case http.MethodPost:
			var body struct {
				OperationID string                         `json:"operation_id"`
				Patch       wire.SessionConfigurationPatch `json:"patch"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requested = body.Patch
			current.McpServers = body.Patch.McpServers
			current.SkillDirs = body.Patch.SkillDirs
			current.SkillRoots = body.Patch.SkillRoots
			revision = "2"
			writeFixture(w, wire.CommandResult{
				OperationId: body.OperationID,
				Outcome:     "committed",
				Configuration: &wire.SessionConfiguration{
					SessionId: "main",
					Revision:  revision,
					Profile:   &current,
				},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	s.info.Capabilities = []string{atomicCapabilities}
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: noTools{}, NotebookDirectory: notebook, Plugins: selection}); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(notebook, ".agents", "skills")
	if info, err := os.Stat(skillDir); err != nil || !info.IsDir() {
		t.Fatalf("Notebook Bot skill directory was not prepared: %v", err)
	}
	if len(s.profile.SkillDirs) != 1 || s.profile.SkillDirs[0] != skillDir {
		t.Fatalf("native profile skill_dirs = %q, want %q", s.profile.SkillDirs, skillDir)
	}
	if len(s.profile.McpServers) != 1 {
		t.Fatalf("native profile MCP servers = %+v", s.profile.McpServers)
	}
	if s.profile.McpServers[0].Name != plugins.RuntimeName("reviewed", "search") || s.profile.McpServers[0].Transport != "streamable_http" {
		t.Fatalf("managed plugin server was not retained: %+v", s.profile.McpServers[0])
	}
	writeWorkspaceMCP(t, notebook, `{"mcpServers":`)
	fresh := New(Options{Directory: t.TempDir()})
	if err := fresh.ConfigureBotTools(&api.ToolConnection{Host: noTools{}, NotebookDirectory: notebook, Plugins: selection}); err != nil {
		t.Fatal("malformed optional project file blocked fresh profile configuration", err)
	}
	if err := s.UpdateBotPlugins(t.Context(), selection); err != nil {
		t.Fatal("malformed optional project file blocked managed plugin reconciliation", err)
	}
	if len(requested.McpServers) != 1 || requested.McpServers[0].Name != plugins.RuntimeName("reviewed", "search") || len(requested.SkillDirs) != 1 || requested.SkillDirs[0] != skillDir {
		t.Fatalf("managed plugin reconciliation changed scope: %+v", requested)
	}
}

func writeWorkspaceMCP(t *testing.T, notebook, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(notebook, ".mcp.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// Exercise native discovery against the external Host used by smoke-caelis.
func verifyNativeProjectMCP(t *testing.T, ctx context.Context, bot *Session, host *client, config *api.ToolConnection, settings api.RuntimeSettings, root, home, model string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	fixture, _ := nativeMCPDirectoryFixture(t)
	defer fixture.Close()
	const projectName = "notebook_fixture"
	const globalName = "host_default_fixture"
	document := func(name string) string {
		return fmt.Sprintf(`{"mcpServers":{%q:{"url":%q}}}`, name, fixture.URL)
	}
	notebook := config.NotebookDirectory
	path := filepath.Join(notebook, ".mcp.json")
	defer os.Remove(path)
	globalDir := filepath.Join(home, ".agents")
	globalPath := filepath.Join(globalDir, "mcp.json")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(document(globalName)), 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(globalPath)
	readTrust := func() wire.StatusConfiguration {
		t.Helper()
		var status wire.StatusSnapshot
		if err := host.json(ctx, "GET", "/status?cwd="+url.QueryEscape(notebook), nil, &status, "", ""); err != nil {
			t.Fatal(err)
		}
		return status.Configuration
	}
	setTrust := func(level wire.WorkspaceTrustLevel) {
		t.Helper()
		status := readTrust()
		op := "fixture-notebook-" + string(level)
		request := wire.WorkspaceTrustRequest{OperationId: &op, Cwd: notebook, WorkspaceKey: notebook, TrustLevel: level, ExpectedRevision: &status.Revision}
		var result wire.CommandResult
		if err := host.json(ctx, "POST", "/configuration/workspace-trust", request, &result, op, string(status.Revision)); err != nil || !succeeded(result.Outcome) {
			t.Fatal("native workspace trust fixture", result.Outcome, err)
		}
	}
	lookup := func(status wire.SessionMCPStatus, name string) wire.SessionMCPServerStatus {
		for _, server := range status.Servers {
			if server.Name == name {
				return server
			}
		}
		return wire.SessionMCPServerStatus{}
	}
	waitStatus := func(stage, sid string, check func(wire.SessionMCPStatus) bool) wire.SessionMCPStatus {
		t.Helper()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			var status wire.SessionMCPStatus
			err := host.json(ctx, "GET", "/sessions/"+idPath(sid)+"/mcp-status", nil, &status, "", "")
			if err == nil && check(status) {
				return status
			}
			select {
			case <-ctx.Done():
				var observed []string
				for _, server := range status.Servers {
					observed = append(observed, fmt.Sprintf("%s:%s (ready=%v, tools=%d)", server.Name, server.Status, value(server.Ready), len(server.Tools)))
				}
				t.Fatalf("%s: MCP status did not converge (servers=%v, read=%v)", stage, observed, err)
			case <-tick.C:
			}
		}
	}
	ready := func(status wire.SessionMCPStatus, name string) bool {
		server := lookup(status, name)
		return value(server.Ready) && slices.Contains(server.Tools, "lookup_0000")
	}
	if readTrust().WorkspaceTrust != wire.WorkspaceTrustLevelTrusted {
		t.Fatal("resident connection did not default Notebook trust")
	}
	writeWorkspaceMCP(t, notebook, document(projectName))
	submitAcceptance(t, ctx, bot, "CASE_PROJECT_MCP_DEFAULT_TRUST")
	sid := bot.ConversationState().Session
	waitStatus("default Notebook trust", sid, func(status wire.SessionMCPStatus) bool {
		return ready(status, projectName) && ready(status, globalName)
	})
	setTrust(wire.WorkspaceTrustLevelUntrusted)
	untrusted := waitStatus("untrusted project", sid, func(status wire.SessionMCPStatus) bool {
		return ready(status, globalName) && lookup(status, projectName).Name == ""
	})

	setTrust(wire.WorkspaceTrustLevelTrusted)
	trusted := waitStatus("trusted project", sid, func(status wire.SessionMCPStatus) bool { return ready(status, projectName) })
	if !ready(trusted, globalName) || trusted.ConfigurationRevision != untrusted.ConfigurationRevision {
		t.Fatal("native reload changed the explicit profile revision or lost the Host default")
	}
	configRead, err := bot.Configuration(ctx)
	if err != nil || len(configRead.Profile.McpServers) != 0 {
		t.Fatal("native discovery entered the explicit profile", err)
	}

	var created wire.CommandResult
	op := "fixture-ordinary-notebook"
	if err := host.json(ctx, "POST", "/sessions", wire.CreateSessionRequest{OperationId: &op, Cwd: &notebook, WorkspaceKey: &notebook, Model: &model}, &created, op, ""); err != nil || !succeeded(created.Outcome) {
		t.Fatal("ordinary Notebook Session creation", created.Outcome, err)
	}
	ordinarySID := value(created.SessionId)
	op = "fixture-ordinary-notebook-prompt"
	var result wire.CommandResult
	if err := host.json(ctx, "POST", "/sessions/"+idPath(ordinarySID)+"/prompt", wire.PromptRequest{OperationId: &op, Input: pointer("CASE_PROJECT_MCP_ORDINARY")}, &result, op, ""); err != nil || !succeeded(result.Outcome) {
		t.Fatal("ordinary Notebook Session prompt", result.Outcome, err)
	}
	waitStatus("ordinary Notebook session", ordinarySID, func(status wire.SessionMCPStatus) bool {
		return ready(status, projectName) && ready(status, globalName)
	})

	workerDir := filepath.Join(root, "mcp-worker")
	if err := os.Mkdir(workerDir, 0700); err != nil {
		t.Fatal(err)
	}
	workerBot := New(Options{Directory: filepath.Join(root, "mcp-worker-bot"), Settings: settings, Execution: api.ExecutionSettings{Model: model}})
	if err := workerBot.ConfigureBotTools(config); err != nil {
		t.Fatal(err)
	}
	if err := workerBot.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer workerBot.Close(context.Background())
	workerBot.mu.Lock()
	call := wire.SessionToolCall{SessionId: workerBot.state.Session.SessionId, PrincipalId: workerBot.state.PrincipalID, Source: wire.SessionSource{Kind: "user", OperationId: "fixture-worker-source"}}
	workerBot.mu.Unlock()
	workerCtx := context.WithValue(ctx, invocationKey{}, call)
	task, err := workerBot.StartWork(workerCtx, api.WorkStart{ID: "mcp-worker", Workspace: workerDir, TaskStart: api.TaskStart{RequestID: "fixture-mcp-worker", Title: "Native MCP scope fixture", Prompt: "CASE_PROJECT_MCP_WORKER"}})
	if err != nil || task.Outcome != "accepted" {
		t.Fatal("native Worker startup", task.Outcome, err)
	}
	workerBot.mu.Lock()
	workerSID := workerBot.state.Workers[task.ID].Binding.SessionId
	workerBot.mu.Unlock()
	workerStatus := waitStatus("independent Worker", workerSID, func(status wire.SessionMCPStatus) bool { return ready(status, globalName) })
	if lookup(workerStatus, projectName).Name != "" {
		t.Fatal("Notebook MCP was inherited by a Worker in its own workspace")
	}

	// The Notebook editor removes a server by saving the remaining map.
	writeWorkspaceMCP(t, notebook, `{"mcpServers":{}}`)
	waitStatus("project removal", sid, func(status wire.SessionMCPStatus) bool {
		return ready(status, globalName) && lookup(status, projectName).Name == ""
	})
	writeWorkspaceMCP(t, notebook, `{"mcpServers":`)
	fresh := New(Options{RequireApproval: true, Directory: filepath.Join(root, "mcp-bad-project-bot"), Settings: settings, Execution: api.ExecutionSettings{Model: model, Effort: "low", ApprovalMode: "workspace-write"}})
	if err := fresh.ConfigureBotTools(config); err != nil {
		t.Fatal("optional project file blocked Bot profile configuration", err)
	}
	if err := fresh.Connect(ctx); err != nil {
		t.Fatal("optional project file blocked native session startup", err)
	}
	defer fresh.Close(context.Background())
	// Connect creates the native session. A completed turn also verifies that
	// a malformed optional config did not block activation of its runtime.
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	waitSnapshot := func(stage string, check func(api.Snapshot) bool) api.Snapshot {
		t.Helper()
		for {
			snapshot := fresh.Snapshot()
			if check(snapshot) {
				return snapshot
			}
			select {
			case <-ctx.Done():
				t.Fatalf("%s: phase=%s connection=%s canSend=%v turnObserved=%v", stage, snapshot.Phase, snapshot.Connection, snapshot.CanSend, snapshot.CurrentTurn != "")
			case <-tick.C:
			}
		}
	}
	waitSnapshot("malformed project startup", func(s api.Snapshot) bool { return s.CanSend })
	receipt, err := fresh.Submit(ctx, api.Submission{ID: "fixture-mcp-bad-startup", Text: "CASE_PROJECT_MCP_BAD_STARTUP"}, nil)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("malformed project prompt", receipt.Outcome, err)
	}
	waitSnapshot("malformed project turn", func(s api.Snapshot) bool {
		// A released Runtime returns to idle. Its completed assistant output
		// survives that transition in this fresh, single-request Session.
		return s.CanSend && slices.ContainsFunc(s.Items, func(item api.Item) bool {
			return item.Kind == "assistant" && item.Text == "ACCEPTANCE_COMPLETE"
		})
	})
	waitStatus("malformed project startup", fresh.ConversationState().Session, func(status wire.SessionMCPStatus) bool {
		return ready(status, globalName) && lookup(status, projectName).Name == ""
	})
}
