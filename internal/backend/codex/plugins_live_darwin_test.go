package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"golang.org/x/sys/unix"
)

// A private App Server and synthetic model exercise the reviewed package through
// the actual Bot workspace projection. No account or global Codex files are used.
func TestNativeReviewedPluginActivation(t *testing.T) {
	binary := os.Getenv("CAELIS_BOT_TEST_CODEX")
	if binary == "" {
		t.Skip("set CAELIS_BOT_TEST_CODEX to the installed CLI")
	}
	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var mu sync.Mutex
	var requests []string
	var readSkillPath string
	modelReceipt := os.Getenv("CAELIS_BOT_PLUGIN_MODEL_RECEIPT_ID")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		mu.Lock()
		requests = append(requests, string(body))
		n := len(requests)
		mu.Unlock()
		id := fmt.Sprintf("plugin-%d", n)
		item := map[string]any{"id": id, "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "PLUGIN_TURN_OK", "annotations": []any{}}}}
		if n == 1 && modelReceipt != "" {
			command := "cat '" + strings.ReplaceAll(readSkillPath, "'", "'\"'\"'") + "'"
			args, _ := json.Marshal(map[string]any{"cmd": command, "max_output_tokens": 8000})
			item = map[string]any{"id": id, "type": "function_call", "name": "exec_command", "call_id": id, "arguments": string(args)}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": id, "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{item}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}},
		} {
			encoded, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
			w.(http.Flusher).Flush()
		}
	}))
	defer provider.Close()
	config := fmt.Sprintf("model = \"plugin-fixture\"\nmodel_provider = \"plugin-fixture\"\n[model_providers.plugin-fixture]\nname = \"Local fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n", provider.URL)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := plugins.Open(filepath.Join(root, "Plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Mutate(t.Context(), "markdown-work", "install", nil); err != nil {
		t.Fatal(err)
	}
	// Test-only functional MCP services. They are never entries in Bot's
	// reviewed product catalog and touch only this temporary directory.
	serviceRoot := filepath.Join(root, "mcp-fixture")
	if err = os.MkdirAll(serviceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	serviceScript := filepath.Join(serviceRoot, "service.py")
	const fixtureMCP = `import json,sys
domain=sys.argv[1]
tool="lookup_fixture" if domain=="documents" else "add_fixture"
for line in sys.stdin:
 try:
  request=json.loads(line); method=request.get("method"); rid=request.get("id")
  if rid is None: continue
  if method=="initialize": result={"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"fixture_"+domain,"version":"1"}}
  elif method=="tools/list": result={"tools":[{"name":tool,"description":"Synthetic "+domain+" test tool","inputSchema":{"type":"object","properties":{}},"annotations":{"readOnlyHint":domain=="documents"}}]}
  elif method=="tools/call": result={"content":[{"type":"text","text":"FIXTURE_"+domain.upper()+"_OK"}],"isError":False}
  else: result={}
  sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":rid,"result":result})+"\n"); sys.stdout.flush()
 except Exception as error:
  sys.stderr.write(str(error)+"\n")
`
	if err = os.WriteFile(serviceScript, []byte(fixtureMCP), 0600); err != nil {
		t.Fatal(err)
	}
	selection := manager.Selection()
	readSkillPath = filepath.Join(selection.SkillRoots[0], "SKILL.md")
	badSkill := filepath.Join(root, "versions", "test-bad", "v1", "skills", "bad")
	if err = os.MkdirAll(badSkill, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(badSkill, "SKILL.md"), []byte("---\nname: bad\n---\nBroken test Skill\n"), 0600); err != nil {
		t.Fatal(err)
	}
	selection.SkillRoots = append(selection.SkillRoots, badSkill)
	for _, domain := range []string{"documents", "utility"} {
		selection.Servers = append(selection.Servers, plugins.SelectedServer{PackageID: "test-" + domain, Name: domain, Root: serviceRoot, Data: filepath.Join(root, "PluginData", domain), Server: plugins.Server{Type: "stdio", Command: "python3", Args: []string{"-u", serviceScript, domain}}})
	}
	workspace := filepath.Join(root, "Notebook")
	s := NewSession(SessionOptions{Binary: binary, Directory: workspace, StateFile: filepath.Join(root, "binding.json")})
	if err = s.ConfigureBotTools(&api.ToolConnection{Command: "/usr/bin/false", Args: []string{"--fixture"}, Env: map[string]string{}, NotebookDirectory: workspace, Plugins: selection}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = s.Close(closeCtx)
	}()
	if err = s.Connect(ctx); err != nil {
		var native *NativeError
		if errors.As(s.lastConnectCause, &native) {
			t.Fatal(err, native.Code, native.Message)
		}
		t.Fatal(err, s.Snapshot().ConnectionIssue, s.lastConnectCause)
	}
	var skills any
	if err = callDecode(ctx, s.client, "skills/list", map[string]any{"cwds": []string{workspace}, "forceReload": true}, &skills); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(skills)
	if !strings.Contains(string(encoded), "markdown-work") {
		t.Fatal("reviewed Skill absent from private Bot workspace")
	}
	badIsolated := false
	health := s.BotPluginHealth(ctx)
	for _, issue := range health {
		if issue.Component == "skill" && issue.Name == badSkill && strings.Contains(issue.Message, "description") {
			badIsolated = true
		}
	}
	if !badIsolated {
		t.Fatal("bad test Skill did not report isolated health")
	}
	for _, domain := range []string{"documents", "utility"} {
		name := plugins.RuntimeName("test-"+domain, domain)
		var status any
		if err = callDecode(ctx, s.client, "mcpServerStatus/list", map[string]any{"threadId": s.binding.ThreadID, "serverName": name}, &status); err != nil {
			t.Fatal("functional MCP service failed", domain, err)
		}
		payload, _ := json.Marshal(status)
		tool := "add_fixture"
		if domain == "documents" {
			tool = "lookup_fixture"
		}
		if !strings.Contains(string(payload), tool) {
			t.Fatal("functional MCP service missing", domain)
		}
		inspected, inspectErr := s.BotPluginServer(ctx, name)
		if inspectErr != nil || inspected.State != "connected" || len(inspected.Tools) != 1 || inspected.Tools[0].Name != tool || inspected.Tools[0].Description == "" || inspected.Tools[0].ReadOnlyHint == nil || *inspected.Tools[0].ReadOnlyHint != (domain == "documents") {
			t.Fatal("Bot-scoped lazy tool directory mismatch", domain, inspected, inspectErr)
		}
		var result any
		if err = callDecode(ctx, s.client, "mcpServer/tool/call", map[string]any{"threadId": s.binding.ThreadID, "server": name, "tool": tool, "arguments": map[string]any{}}, &result); err != nil {
			t.Fatal("functional MCP call failed", domain, err)
		}
		payload, _ = json.Marshal(result)
		if !strings.Contains(string(payload), "FIXTURE_"+strings.ToUpper(domain)+"_OK") {
			t.Fatal("functional MCP result missing", domain, string(payload))
		}
	}
	disableAndCheck := func() {
		if err = s.WithBotPluginAdmission(func(apply func(context.Context, plugins.Selection) error) error {
			_, err := manager.Mutate(ctx, "markdown-work", "disable", apply)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err = callDecode(ctx, s.client, "skills/list", map[string]any{"cwds": []string{workspace}, "forceReload": true}, &skills); err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(skills)
		if strings.Contains(string(encoded), "markdown-work") {
			t.Fatal("disabled Skill remained in private Bot workspace")
		}
	}
	// Repeated runtime refreshes must not leave descriptors behind in the Bot
	// host. This checks the resident adapter while the two MCP services stay up.
	countFDs := func() int {
		count := 0
		for fd := 0; fd < 1024; fd++ {
			if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err == nil {
				count++
			}
		}
		return count
	}
	beforeFDs := countFDs()
	for range 12 {
		if err = callDecode(ctx, s.client, "config/mcpServer/reload", nil, nil); err != nil {
			t.Fatal("MCP reload cycle failed", err)
		}
	}
	afterFDs := countFDs()
	if afterFDs > beforeFDs+4 {
		t.Fatal("Bot host descriptors accumulated across MCP reloads", beforeFDs, afterFDs)
	}
	// After an interrupted synthetic turn, verify the live protocol and reload
	// without submitting another model request or replacing the original ID.
	if modelReceipt == "" || os.Getenv("CAELIS_BOT_PLUGIN_PROTOCOL_ONLY") == "1" {
		disableAndCheck()
		return
	}
	if modelReceipt == "plugin-first" {
		t.Fatal("the original interrupted model receipt must not be replayed")
	}
	r, err := s.Submit(ctx, api.Submission{ID: modelReceipt, Text: "Write a Markdown note."}, nil)
	if err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	var revision uint64
	for {
		v, waitErr := s.WaitSnapshot(ctx, revision)
		if waitErr != nil {
			t.Fatal(waitErr)
		}
		revision = v.Revision
		if v.Phase == "completed" {
			break
		}
		if v.Phase == "failed" || len(v.Approvals) > 0 {
			t.Fatal("private Codex plugin turn failed", v.Phase, v.Message)
		}
	}
	mu.Lock()
	first := append([]string(nil), requests...)
	mu.Unlock()
	if len(first) != 2 || !strings.Contains(first[0], "markdown-work") || strings.Contains(first[0], "Start with the user's purpose") || !strings.Contains(first[1], "Start with the user's purpose") {
		t.Fatal("Skill body did not load only after the native file call", len(first))
	}
	disableAndCheck()
}
