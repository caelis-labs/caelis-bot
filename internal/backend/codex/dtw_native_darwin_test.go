package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Opt-in acceptance uses a private Codex App Server and the installed,
// signed rc.4 package. The provider is local and no account is read.
func TestNativeDesktopWorldPluginDirectory(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	bot := os.Getenv("CAELIS_BOT_TEST_BOT_BINARY")
	packageRoot := os.Getenv("CAELIS_BOT_TEST_DTW_ROOT")
	pluginStore := os.Getenv("CAELIS_BOT_TEST_DTW_STORE")
	if binary == "" || bot == "" || packageRoot == "" || pluginStore == "" {
		t.Skip("set private Codex, DevBot, Desktop World package and plugin store paths")
	}
	p, err := plugins.Load(packageRoot)
	if err != nil || p.Manifest.Name != "desktop-world" || p.Manifest.Version != "0.1.0-rc.4" || len(p.Skills) != 1 || len(p.Servers) != 1 || p.Servers[0].Type != "stdio" {
		t.Fatal("invalid installed rc.4 package", err)
	}
	var saved struct {
		Installed map[string]struct {
			Root    string
			Enabled bool
		}
	}
	state, err := os.ReadFile(filepath.Join(pluginStore, "state.json"))
	if err != nil || json.Unmarshal(state, &saved) != nil || !saved.Installed["desktop-world"].Enabled || filepath.Base(packageRoot) != saved.Installed["desktop-world"].Root {
		t.Fatal("supplied rc.4 package is not the enabled generation", err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer provider.Close()
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	config := fmt.Sprintf("model = \"plugin-fixture\"\nmodel_provider = \"plugin-fixture\"\n[model_providers.plugin-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "Notebook")
	selection := plugins.Selection{Revision: 1, SkillRoots: p.Skills, Servers: []plugins.SelectedServer{{PackageID: "desktop-world", Name: p.Servers[0].Name, Root: p.Root, Data: filepath.Join(pluginStore, "data", "desktop-world"), Server: p.Servers[0]}}}
	session := NewSession(SessionOptions{Binary: binary, Directory: workspace, StateFile: filepath.Join(root, "binding.json")})
	if err := session.ConfigureBotTools(&api.ToolConnection{Command: bot, Args: []string{"--fixture"}, Env: map[string]string{"CAELIS_BOT_TOOL_CATALOG": "[]"}, NotebookDirectory: workspace, Plugins: selection}); err != nil {
		t.Fatal(err)
	}
	project, err := os.ReadFile(filepath.Join(workspace, ".codex", "config.toml"))
	if err != nil || !strings.Contains(string(project), "--plugin-mcp") || strings.Contains(string(project), "command = "+fmt.Sprintf("%q", filepath.Join(packageRoot, "bin", "dtw"))) {
		t.Fatal("Desktop World escaped the Bot generation bridge", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	defer func() { _ = session.Close(context.Background()) }()
	if err := session.Connect(ctx); err != nil {
		var native *NativeError
		if errors.As(session.lastConnectCause, &native) {
			t.Fatal(err, native.Code, native.Message)
		}
		t.Fatal(err, session.Snapshot().ConnectionIssue, session.lastConnectCause)
	}
	var skills any
	if err := callDecode(ctx, session.client, "skills/list", map[string]any{"cwds": []string{workspace}, "forceReload": true}, &skills); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(skills), "desktop-world") {
		t.Fatal("rc.4 Skill missing from resident Codex")
	}
	name := plugins.RuntimeName("desktop-world", p.Servers[0].Name)
	var detail plugins.ServerDetail
	for {
		detail, err = session.BotPluginServer(ctx, name)
		if err == nil && detail.State == "connected" && len(detail.Tools) == 1 {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("rc.4 MCP did not connect", detail.State, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if detail.Tools[0].Name != "exec" {
		t.Fatal("rc.4 must expose only exec", detail.Tools)
	}
	processes, err := exec.Command("/bin/ps", "-ww", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		t.Fatal("cannot inspect private MCP process ancestry", err)
	}
	var parent string
	for _, line := range strings.Split(string(processes), "\n") {
		if strings.Contains(line, packageRoot+"/bin/dtw") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				parent = fields[1]
			}
		}
	}
	bridge := false
	for _, line := range strings.Split(string(processes), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == parent && strings.Contains(line, bot+" --plugin-mcp") {
			bridge = true
		}
	}
	if parent == "" || !bridge {
		t.Fatal("DTW was not spawned by the signed Bot plugin bridge")
	}
	if err := session.UpdateBotPlugins(ctx, plugins.Selection{Revision: 2}); err != nil {
		t.Fatal(err)
	}
	if err := callDecode(ctx, session.client, "skills/list", map[string]any{"cwds": []string{workspace}, "forceReload": true}, &skills); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(skills), "desktop-world") {
		t.Fatal("disabled rc.4 Skill remained in Codex")
	}
	if err := callDecode(ctx, session.client, "mcpServer/tool/call", map[string]any{"threadId": session.binding.ThreadID, "server": name, "tool": "exec", "arguments": map[string]any{"operation": "permissions"}}, nil); err == nil {
		t.Fatal("disabled rc.4 exec remained callable on the original thread")
	}
}
