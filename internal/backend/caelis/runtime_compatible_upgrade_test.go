//go:build darwin

package caelis

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// Opt-in consumer upgrade: real compatible releases/private Store, synthetic
// provider, one counted private callback, retained Worker and fresh MCP discovery.
func TestCompatibleRuntimeUpgrade(t *testing.T) {
	previous, current := os.Getenv("CAELIS_BOT_TEST_PREVIOUS_BINARY"), os.Getenv("CAELIS_BOT_TEST_BINARY")
	if previous == "" || current == "" {
		t.Skip("set previous/current formal Core binaries for isolated compatible upgrade")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	root := t.TempDir()
	if base := os.Getenv("CAELIS_BOT_TEST_EVIDENCE_DIR"); base != "" {
		var err error
		root, err = os.MkdirTemp(base, "runtime-upgrade-")
		if err != nil {
			t.Fatal(err)
		}
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: previous, CaelisStore: filepath.Join(root, "store")}
	environment := newExecutionFixture(t, root)
	start := exec.CommandContext(ctx, previous, "service", "start", "--store-dir", settings.CaelisStore, "--format", "json")
	start.Dir, start.Env = root, environment.env
	runtimeenv.BackgroundCommand(start)
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("private previous Host start: %v %s", err, output)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		command := exec.CommandContext(cleanup, current, "service", "stop", "--store-dir", settings.CaelisStore, "--format", "json")
		command.Env, command.Dir = environment.env, root
		runtimeenv.BackgroundCommand(command)
		_ = command.Run()
	}()
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	host, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = initialize(ctx, host); err != nil {
		t.Fatal("previous Host must already satisfy the Bot contract", err)
	}
	model := newAcceptanceModel()
	defer func() {
		model.mu.Lock()
		defer model.mu.Unlock()
		data, _ := json.MarshalIndent(model.requests, "", "  ")
		_ = os.WriteFile(filepath.Join(root, "model-requests.json"), data, 0600)
	}()
	provider := httptest.NewServer(nativeDTWModel(model))
	defer provider.Close()
	var status wire.StatusSnapshot
	if err = host.json(ctx, "GET", "/status", nil, &status, "", ""); err != nil {
		t.Fatal(err)
	}
	op := "compatible-configure-provider"
	var configured wire.CommandResult
	if err = host.json(ctx, "POST", "/configuration/connect-model", wire.ConnectModelRequest{OperationId: &op, ExpectedRevision: &status.Configuration.Revision, Config: wire.ConnectConfig{Provider: "custom-responses", Model: "gpt-5.4-mini", BaseUrl: pointer(provider.URL + "/v1"), ApiKey: pointer("SYNTHETIC_ONLY")}}, &configured, op, string(status.Configuration.Revision)); err != nil || !succeeded(configured.Outcome) {
		t.Fatal("private provider configuration", err)
	}
	selector := fixtureModelSelector(t, ctx, host, "gpt-5.4-mini")
	opts := Options{RequireApproval: true, Directory: filepath.Join(root, "resident"), Settings: settings, Execution: api.ExecutionSettings{Model: selector, ApprovalMode: "workspace-write"}}
	var effects atomic.Int32
	tools := &acceptanceTools{defs: fixtureDefinitions("string"), call: func(context.Context, string, json.RawMessage) api.ToolResult {
		effects.Add(1)
		return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "PRIVATE_CALLBACK_COMPLETE"}}}
	}}
	connection := &api.ToolConnection{ApprovedTools: []string{"FixtureLookup"}, Host: tools}
	resident := New(opts)
	defer resident.Close(context.Background())
	if err = resident.ConfigureBotTools(connection); err != nil {
		t.Fatal(err)
	}
	if err = resident.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return resident.Snapshot().CanSend })
	model.set("CASE_COMPATIBLE_BEFORE", modelStep{Name: "FixtureLookup", Args: map[string]string{"key": "before"}})
	submitAcceptance(t, ctx, resident, "CASE_COMPATIBLE_BEFORE")
	originalID := "case_compatible_before"
	var original wire.CommandResult
	if err = host.json(ctx, "GET", "/operations/"+originalID, nil, &original, "", ""); err != nil || !succeeded(original.Outcome) || effects.Load() != 1 {
		t.Fatal("original counted callback/receipt", err)
	}
	resident.mu.Lock()
	before := clone(resident.state)
	resident.mu.Unlock()
	workOpts := Options{RequireApproval: true, Directory: filepath.Join(root, "worker-owner"), Settings: settings, WorkSourceIdentity: "compatible-upgrade-owner", WorkExecution: api.WorkExecutionSettings{Model: selector}}
	owner, err := NewWorkOwner(workOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	if err = owner.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	model.set("CASE_COMPATIBLE_WORKER", modelStep{Reply: "ORIGINAL_WORKER_COMPLETE"})
	task, err := owner.StartWork(ctx, api.WorkStart{ID: "compatible-worker", Workspace: workspace, TaskStart: api.TaskStart{RequestID: "compatible-worker-original", Title: "Private upgrade fixture", Prompt: "CASE_COMPATIBLE_WORKER"}})
	if err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { v, e := owner.ReadWork(ctx, task.ID); return e == nil && v.Status == "completed" })
	owner.engine.mu.Lock()
	workerSession := owner.engine.state.Workers[task.ID].Binding.SessionId
	owner.engine.mu.Unlock()
	workerRequests := len(model.seen("CASE_COMPATIBLE_WORKER"))
	entered, release := make(chan struct{}, 1), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	model.set("CASE_COMPATIBLE_BUSY", modelStep{Entered: entered, Block: release, Reply: "ORIGINAL_BUSY_TURN_COMPLETE"})
	if receipt, err := resident.Submit(ctx, api.Submission{ID: "compatible-busy-original", Text: "CASE_COMPATIBLE_BUSY"}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("original busy Turn", err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err = caelisruntime.Manage(ctx, "start", current, settings.CaelisStore); err != nil {
		t.Fatal("Bot public candidate convergence", err)
	}
	still, _, err := Discover(settings)
	if err != nil || still.InstanceID != d.InstanceID {
		t.Fatal("candidate replaced an active compatible Host", err)
	}
	if err = resident.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	settings.CLIPath = current
	opts.Settings = settings
	upgraded := New(opts)
	defer upgraded.Close(context.Background())
	if err = upgraded.ConfigureBotTools(connection); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.Connect(ctx); err != nil {
		t.Fatal("compatible busy Host cannot reject the new Bot", err)
	}
	close(release)
	released = true
	waitAcceptance(t, ctx, func() bool { v, _, e := Discover(settings); return e == nil && v.InstanceID != d.InstanceID })
	if err = upgraded.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return upgraded.Snapshot().CanSend })
	upgraded.mu.Lock()
	after, currentClient := clone(upgraded.state), upgraded.client
	upgraded.mu.Unlock()
	if before.Session.SessionId != after.Session.SessionId || before.StoreID != after.StoreID || before.PrincipalID != after.PrincipalID || effects.Load() != 1 || !reflect.DeepEqual(before.Operations[originalID], after.Operations[originalID]) {
		t.Fatal("upgrade changed the original binding/receipt or replayed its callback")
	}
	var retained wire.CommandResult
	if err = currentClient.json(ctx, "GET", "/operations/"+originalID, nil, &retained, "", ""); err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("original Core receipt changed", err)
	}
	workOpts.Settings = settings
	restored, err := NewWorkOwner(workOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close(context.Background())
	if err = restored.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := restored.ReadWork(ctx, task.ID); err != nil || v.Status != "completed" {
		t.Fatal("original Worker restore", err)
	}
	restored.engine.mu.Lock()
	retainedWorker := restored.engine.state.Workers[task.ID].Binding.SessionId
	restored.engine.mu.Unlock()
	if workerSession != retainedWorker || workerRequests != len(model.seen("CASE_COMPATIBLE_WORKER")) {
		t.Fatal("Worker was rebound or replayed")
	}
	entered, release = make(chan struct{}, 1), make(chan struct{})
	released = false
	key := "CASE_COMPATIBLE_DISCOVER"
	model.set(key, modelStep{Name: "tool_search", Args: map[string]any{"query": "Desktop World apps exec", "limit": 2}, Entered: entered, Block: release}, modelStep{Name: "mcp_call", BuildArgs: func() any {
		return map[string]any{"tool_name": discoveredDTWTool(t, model, key, "apps"), "arguments": map[string]any{"query": "Bot Runtime Probe"}}
	}})
	if _, err = upgraded.Submit(ctx, api.Submission{ID: "compatible-fresh-discovery", Text: key}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	directory := waitFixedDTW(t, ctx, currentClient, after.Session.SessionId)
	close(release)
	released = true
	waitAcceptance(t, ctx, func() bool { return upgraded.Snapshot().CanSend && len(model.seen(key)) >= 3 })
	evidence := map[string]any{"resident_session": after.Session.SessionId, "worker_session": retainedWorker, "original_callback_count": effects.Load(), "original_receipt": retained, "worker_requests_before": workerRequests, "worker_requests_after": len(model.seen("CASE_COMPATIBLE_WORKER")), "upgraded_mcp_status": directory, "model_decisions": "synthetic", "mcp_discovery_and_apps": "real"}
	data, _ := json.MarshalIndent(evidence, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "upgrade-summary.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("compatible busy Host deferred to idle; same resident/Worker/receipt, one callback; fresh DTW discovery; evidence", root)
}
