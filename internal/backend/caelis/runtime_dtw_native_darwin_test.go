package caelis

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// Adapter acceptance only: synthetic provider decisions, real formal Core and
// fixed DTW MCP/OS reads. Core's existing real-agent task is separate evidence.
func TestNativeRuntimeDesktopWorld(t *testing.T) {
	bin := os.Getenv("CAELIS_BOT_TEST_BINARY")
	if bin == "" {
		t.Skip("set CAELIS_BOT_TEST_BINARY to the formal pinned Core")
	}
	app := os.Getenv("CAELIS_BOT_TEST_DTW_APP")
	if app == "" {
		t.Skip("set CAELIS_BOT_TEST_DTW_APP to an authorized isolated read-only app")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	root := t.TempDir()
	if base := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR"); base != "" {
		var err error
		root, err = os.MkdirTemp(base, "runtime-dtw-")
		if err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	model := newAcceptanceModel()
	defer func() {
		model.mu.Lock()
		b, _ := json.MarshalIndent(model.requests, "", "  ")
		model.mu.Unlock()
		_ = os.WriteFile(filepath.Join(root, "model-requests.json"), b, 0600)
	}()
	provider := httptest.NewServer(nativeDTWModel(model))
	defer provider.Close()
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: bin, CaelisStore: filepath.Join(root, "store")}
	env := newExecutionFixture(t, root)
	cmd := exec.CommandContext(ctx, bin, "serve", "--store-dir", settings.CaelisStore, "--listen", "127.0.0.1:0")
	cmd.Dir, cmd.Env = root, env.env
	log, err := os.OpenFile(filepath.Join(root, "host.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	waitAcceptance(t, ctx, func() bool { _, _, e := Discover(settings); return e == nil })
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = initialize(ctx, client); err != nil {
		t.Fatal(err)
	}
	var status wire.StatusSnapshot
	if err = client.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "configure-bot-adapter-provider"
	var result wire.CommandResult
	if err = client.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-responses", Model: "gpt-5.4-mini", BaseUrl: pointer(provider.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &result, op, string(status.Configuration.Revision)); err != nil || !succeeded(result.Outcome) {
		t.Fatal("provider setup", err)
	}
	selector := fixtureModelSelector(t, ctx, client, "gpt-5.4-mini")
	s := New(Options{RequireApproval: true, Directory: filepath.Join(root, "resident"), Settings: settings, Execution: api.ExecutionSettings{Model: selector, ApprovalMode: "workspace-write"}})
	if err = s.ConfigureBotTools(&api.ToolConnection{Host: noTools{}}); err != nil {
		t.Fatal(err)
	}
	if err = s.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	entered, release := make(chan struct{}, 1), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	key := "CASE_DTW_RUNTIME_RESIDENT"
	skill := ""
	model.set(key, modelStep{Name: "tool_search", Args: map[string]any{"query": "Desktop World apps exec", "limit": 2}, Entered: entered, Block: release}, modelStep{Name: "Skill", BuildArgs: func() any { return map[string]any{"name": skill} }}, modelStep{Name: "mcp_call", BuildArgs: func() any {
		return map[string]any{"tool_name": discoveredDTWTool(t, model, key, "apps"), "arguments": map[string]any{"query": app}}
	}})
	if r, e := s.Submit(ctx, api.Submission{ID: "runtime-resident-original", Text: key}, nil); e != nil || r.Outcome != "accepted" {
		t.Fatal("resident submit", e)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	sid := s.ConversationState().Session
	directory := waitFixedDTW(t, ctx, client, sid)
	for _, v := range directory.Skills {
		if strings.HasSuffix(v.Path, "/desktop-world/SKILL.md") && v.Status == "ready" {
			skill = value(v.Name)
		}
	}
	if skill == "" {
		t.Fatal("fixed Skill absent")
	}
	close(release)
	released = true
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend && len(model.seen(key)) >= 4 })
	found := false
	for _, r := range model.seen(key) {
		walkDTWJSON(r, func(v map[string]any) {
			for _, x := range v {
				if x == app {
					found = true
				}
			}
		})
	}
	if !found {
		t.Fatal("probe app was not returned by native apps")
	}
	execKey := "CASE_DTW_RUNTIME_EXEC"
	source := "const observation=await app.look(); return {probe:'BOT_RUNTIME_RESIDENT',observed:!!observation};"
	setFixedDTWRead(t, model, execKey, app, source, nil, nil)
	if _, err = s.Submit(ctx, api.Submission{ID: "runtime-read-original", Text: execKey}, nil); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return len(s.Snapshot().Approvals) == 1 })
	before := s.Snapshot().Approvals[0]
	if err = s.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return len(s.Snapshot().Approvals) == 1 })
	after := s.Snapshot().Approvals[0]
	if !reflect.DeepEqual(before, after) {
		t.Fatal("original approval/options changed on reconnect")
	}
	approveDTWApp(t, ctx, s, app, source)
	waitAcceptance(t, ctx, func() bool {
		return s.Snapshot().CanSend && dtwCompletedCall(model, execKey, "BOT_RUNTIME_RESIDENT") != ""
	})
	var configured wire.SessionConfiguration
	if err = client.json(ctx, "GET", "/sessions/"+idPath(sid)+"/configuration", nil, &configured, "", ""); err != nil || configured.Profile == nil {
		t.Fatal("resident configuration", err)
	}
	if len(configured.Profile.Plugins) != 0 || len(configured.Profile.McpServers) != 0 || len(configured.Profile.SkillRoots) != 0 {
		t.Fatal("Bot injected DTW selection/bridge/Skill")
	}
	workerKey := "CASE_DTW_RUNTIME_WORKER"
	went, wrelease := make(chan struct{}, 1), make(chan struct{})
	wreleased := false
	defer func() {
		if !wreleased {
			close(wrelease)
		}
	}()
	wsource := "const observation=await app.look(); return {probe:'BOT_RUNTIME_WORKER',observed:!!observation};"
	setFixedDTWRead(t, model, workerKey, app, wsource, went, wrelease)
	owner, err := NewWorkOwner(Options{RequireApproval: true, Directory: filepath.Join(root, "worker-owner"), Settings: settings, WorkSourceIdentity: "bot-native-worker", WorkExecution: api.WorkExecutionSettings{Model: selector}})
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	workspace := filepath.Join(root, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	task, err := owner.StartWork(ctx, api.WorkStart{ID: "runtime-worker", Workspace: workspace, TaskStart: api.TaskStart{RequestID: "runtime-worker-original", Title: "Read-only Runtime capability", Prompt: workerKey}})
	if err != nil {
		t.Fatal("original Worker start", err)
	}
	owner.engine.mu.Lock()
	wsid := owner.engine.state.Workers[task.ID].Binding.SessionId
	owner.engine.mu.Unlock()
	select {
	case <-went:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitFixedDTW(t, ctx, client, wsid)
	close(wrelease)
	wreleased = true
	waitAcceptance(t, ctx, func() bool { v, e := owner.ReadWork(ctx, task.ID); return e == nil && v.Status == "completed" })
	if dtwCompletedCall(model, workerKey, "BOT_RUNTIME_WORKER") == "" || len(model.seen("DTW_GUARDIAN")) != 1 {
		t.Fatal("Worker actual exec output or separate Guardian app decision absent")
	}
	var wc wire.SessionConfiguration
	if err = client.json(ctx, "GET", "/sessions/"+idPath(wsid)+"/configuration", nil, &wc, "", ""); err != nil || wc.Profile != nil || wc.Native == nil || wc.Native.Plugins != nil {
		t.Fatal("Bot overrode native Worker configuration", err)
	}
	evidence := map[string]any{"resident_session": sid, "worker_session": wsid, "resident_call": dtwCompletedCall(model, execKey, "BOT_RUNTIME_RESIDENT"), "worker_call": dtwCompletedCall(model, workerKey, "BOT_RUNTIME_WORKER"), "resident_original_approval": before, "worker_guardian_decisions": len(model.seen("DTW_GUARDIAN")), "model_decisions": "synthetic", "mcp_os_reads": "real", "bot_dtw_injection": false}
	b, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "adapter-summary.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	b, _ = json.MarshalIndent(model.requests, "", "  ")
	model.mu.Unlock()
	if err = os.WriteFile(filepath.Join(root, "model-requests.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("formal Core builtin: resident + native Worker, own app decisions, original pending approval reconnect; evidence", root)
}

func setFixedDTWRead(t *testing.T, m *acceptanceModel, key, app, source string, entered chan struct{}, block chan struct{}) {
	m.set(key, modelStep{Name: "tool_search", Args: map[string]any{"query": "Desktop World apps exec", "limit": 2}, Entered: entered, Block: block}, modelStep{Name: "mcp_call", BuildArgs: func() any {
		return map[string]any{"tool_name": discoveredDTWTool(t, m, key, "exec"), "arguments": map[string]any{"app": app, "source": source}}
	}})
}

func waitFixedDTW(t *testing.T, ctx context.Context, c *client, sid string) wire.SessionMCPStatus {
	var d wire.SessionMCPStatus
	waitAcceptance(t, ctx, func() bool {
		if c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/mcp-status", nil, &d, "", "") != nil {
			return false
		}
		fixed := 0
		for _, p := range d.Plugins {
			if value(p.Id) == "desktop-world" && value(p.Version) == "0.3.4" && p.Status == "builtin" {
				fixed++
			}
		}
		if fixed != 1 {
			return false
		}
		for _, server := range d.Servers {
			if server.Name == "desktop-world" {
				return server.Status == "running" && (server.Stale == nil || !*server.Stale) && (server.Refreshing == nil || !*server.Refreshing) && (server.RefreshFailed == nil || !*server.RefreshFailed) && value(server.Warning) == "" && len(server.Tools) == 2
			}
		}
		return false
	})
	return d
}
