package caelis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/botskills"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
)

func TestCaelisTaskStdioHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	if err := bot.RunStdioArgs(os.Args[len(os.Args)-1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Caelis v0.85.0 has no async question projection for a native model Worker.
// This fixture supplies only the question and binding. Its accepted reply
// continues the real Worker through Manager and Core's original Session.
type residentQuestionFixture struct {
	manager  *tasks.Manager
	question api.WorkerQuestion
	replies  atomic.Int32
}

func (f *residentQuestionFixture) WorkerApprovals() []api.Approval { return nil }
func (f *residentQuestionFixture) DecideWorkerApproval(context.Context, api.Decision) (api.Approval, error) {
	return api.Approval{}, fmt.Errorf("unexpected fixture approval")
}
func (f *residentQuestionFixture) WorkerQuestions() []api.WorkerQuestion {
	return []api.WorkerQuestion{f.question}
}
func (f *residentQuestionFixture) AnswerWorkerQuestion(ctx context.Context, id, value, request string) (api.Receipt, error) {
	if id != f.question.ID+"@"+f.question.Binding || value != "Fixture option A" || request != "answer-original-stdio-fixture" {
		return api.Receipt{ID: request, Outcome: "rejected"}, fmt.Errorf("original question identity changed")
	}
	f.replies.Add(1)
	task, err := f.manager.SendTask(ctx, api.TaskMessage{ID: f.question.TaskID, RequestID: request, Prompt: "Worker answer fixture: " + value})
	return api.Receipt{ID: request, Outcome: task.Outcome}, err
}

func privateStdioWorkerRoundTrip(t *testing.T, settings api.RuntimeSettings, mainModel, workerModel string, model *acceptanceModel, review func(string, <-chan struct{})) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 105*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skill, err := botskills.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	executable := os.Getenv("CAELIS_BOT_TEST_APP_BINARY")
	args := []string{"--bot-tools"}
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		args = []string{"-test.run=^TestCaelisTaskStdioHelper$", "--"}
	}
	opts := Options{Directory: filepath.Join(root, "binding"), Settings: settings, Execution: api.ExecutionSettings{Model: mainModel}, WorkExecution: api.WorkExecutionSettings{Model: workerModel}}
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	old := New(opts)
	defer old.Close(context.Background())
	legacy := &api.ToolConnection{Instructions: botskills.Bootstrap(), BuiltinSkillRoots: []string{filepath.Dir(skill)},
		NotebookDirectory: filepath.Join(root, "Notebook")}
	if err := os.Mkdir(legacy.NotebookDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := old.ConfigureBotTools(legacy); err != nil {
		t.Fatal(err)
	}
	if err := old.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return old.Snapshot().CanSend })
	// Seed the original catalog through the released Host, independently of
	// the Bot adapter's callback-free profile assembly.
	legacyCatalog := []wire.SessionToolDefinition{{
		Name: "bot_schedule", Description: "Legacy upgrade fixture", ApprovalPolicy: pointer("direct"),
		InputSchema: wire.JSONObject{"type": "object", "properties": map[string]any{}},
	}}
	originalConfig, err := old.Configuration(ctx)
	if err != nil {
		t.Fatal("read configuration before seeding the legacy catalog", err)
	}
	if seeded, err := old.UpdateConfiguration(ctx, "seed-legacy-upgrade", string(originalConfig.Revision), map[string]any{
		"tools": legacyCatalog, "tools_version": "legacy-upgrade-fixture",
	}); err != nil || len(seeded.Profile.Tools) != len(legacyCatalog) {
		t.Fatal("seed legacy catalog on the released Host", err)
	}
	const legacyCase = "CASE_STDIO_LEGACY_RESIDENT"
	model.set(legacyCase, modelStep{Name: "bot_schedule", Args: map[string]any{}})
	oldID := old.ConversationState().Session
	if receipt, err := old.Submit(ctx, api.Submission{ID: "original-legacy-prompt", Text: legacyCase}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("could not seed pre-upgrade resident turn", receipt, err)
	}
	var legacyCall wire.SessionToolCall
	legacyReady, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
	waitAcceptance(t, legacyReady, func() bool {
		var calls []wire.SessionToolCall
		if err := old.client.json(ctx, "GET", "/sessions/"+idPath(oldID)+"/tool-calls", nil, &calls, "", ""); err != nil {
			t.Fatal("fixture could not inspect the original pending callback", err)
		}
		for _, call := range calls {
			if call.Name == "bot_schedule" && call.State == "pending" && call.Source.OperationId == "original-legacy-prompt" {
				legacyCall = call
				return true
			}
		}
		return false
	})
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(opts)
	defer func() { _ = s.Close(context.Background()) }()
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", &reportLoopRuntime{Session: s}, resident, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	mcpInvocation := make(chan api.ToolInvocation, 1)
	manager.ObserveStartsWithContext(func(ctx context.Context, _ api.Task) {
		call, _ := api.ToolInvocationFromContext(ctx)
		select {
		case mcpInvocation <- call:
		default:
		}
	})
	if err := resident.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	const payload = "private-mcp-worker-input-fixture\n"
	file := filepath.Join(root, "selected.txt")
	if err := os.WriteFile(file, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	service := backend.NewService(s, func(ids []string) ([]api.InputFile, error) {
		if slices.Equal(ids, []string{"selected"}) {
			return []api.InputFile{{Name: "selected.txt", Path: file}}, nil
		}
		return nil, nil
	}, nil, nil, nil)
	if err := service.ConfigureDraft(filepath.Join(root, "draft.json")); err != nil {
		t.Fatal(err)
	}
	service.SetUserSubmitter(resident.SubmitUser)
	uploadCache := filepath.Join(root, "resident-uploads")
	backend.ConfigureWorkerInputs(service, uploadCache)
	uploads, err := workerfiles.OpenInputs(uploadCache)
	if err != nil {
		t.Fatal(err)
	}
	manager.ConfigureInputResolver(func(ctx context.Context, id string) (api.WorkFileContent, error) {
		data, _, err := backend.ReadWorkerInput(ctx, service, id)
		return data, err
	})
	bridge, err := bot.Serve(resident)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	cfg := bridge.Config(executable)
	cfg.Args, cfg.NotebookDirectory = args, legacy.NotebookDirectory
	cfg.Instructions, cfg.BuiltinSkillRoots = botskills.Bootstrap(), []string{filepath.Dir(skill)}
	if err := s.ConfigureBotTools(cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx); err != nil {
		s.mu.Lock()
		for id, op := range s.state.Operations {
			t.Logf("CONNECT %s %s outcome=%s", id, op.Path, op.Outcome)
		}
		typed := clone(s.state.Typed)
		s.mu.Unlock()
		for id, op := range typed {
			var receipt wire.CommandResult
			if op.Outcome == "rejected" && s.client.json(ctx, "GET", "/operations/"+idPath(id), nil, &receipt, "", "") == nil {
				t.Logf("CONNECT %s %s outcome=%s error=%s detail=%s", id, op.Path, receipt.Outcome, receipt.ErrorCode, value(receipt.Detail))
			}
		}
		t.Fatal("connect resident", err)
	}
	resident.Start(s)
	sid := s.ConversationState().Session
	if sid != oldID {
		t.Fatal("private migration replaced the retained resident Session")
	}
	current, err := s.Configuration(ctx)
	if err != nil || !hasNativeTaskMCP(current.Profile) {
		t.Fatal("resident did not migrate to the native task MCP profile", err)
	}
	waitAcceptance(t, ctx, func() bool {
		var retired wire.SessionToolCall
		path := "/sessions/" + idPath(sid) + "/tool-calls/" + idPath(legacyCall.Id)
		if err := s.client.json(ctx, "GET", path, nil, &retired, "", ""); err != nil {
			t.Fatal("fixture could not inspect Core's original retired callback", err)
		}
		return retired.State == "cancelled" && retired.Id == legacyCall.Id && retired.TurnId == legacyCall.TurnId && retired.Result == nil
	})
	t.Log("same resident Session upgraded from an actual pending callback, which Core cancelled without a Bot claim or result")
	readStatus := func(session *Session, sid string) wire.SessionMCPStatus {
		t.Helper()
		var status wire.SessionMCPStatus
		if err := session.client.json(ctx, "GET", "/sessions/"+idPath(sid)+"/mcp-status", nil, &status, "", ""); err != nil {
			t.Fatal(err)
		}
		return status
	}
	taskMCPReady := func(status wire.SessionMCPStatus) bool {
		t.Helper()
		for _, server := range status.Servers {
			if server.Name == privateTasksService {
				if server.Status == "failed" || value(server.RefreshFailed) {
					t.Fatal("Core failed to initialize resident stdio", value(server.Warning))
				}
				return value(server.Ready) && slices.Equal(sortedTools(server.Tools),
					[]string{"bot_delegate", "bot_event", "bot_gesture", "bot_interactions", "bot_schedule", "bot_schedule_update", "bot_tasks"})
			}
		}
		return false
	}
	waitForTaskMCP := func(session *Session) {
		t.Helper()
		ready, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		waitAcceptance(t, ready, func() bool { return taskMCPReady(readStatus(session, session.ConversationState().Session)) })
	}
	const skillCase = "CASE_STDIO_NATIVE_SKILL"
	model.set(skillCase, modelStep{Name: "Skill", Args: map[string]string{"name": "bot-core"}})
	submitAcceptance(t, ctx, s, skillCase)
	skillRequests := model.seen(skillCase)
	if len(skillRequests) != 2 {
		t.Fatal("native bot-core load did not complete", len(skillRequests))
	}
	firstSkill, _ := json.Marshal(skillRequests[0])
	loadedSkill, _ := json.Marshal(skillRequests[1])
	if strings.Contains(string(firstSkill), "# Act as a continuing personal assistant") ||
		!strings.Contains(string(loadedSkill), "# Act as a continuing personal assistant") {
		t.Fatal("bot-core body was not loaded progressively")
	}
	waitForTaskMCP(s)
	run := func(key, tool string, args map[string]any, files []api.InputFile) {
		t.Helper()
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
		// The provider fixture responds immediately. Wait for Core's native
		// asynchronous directory when a replacement resident first activates.
		release := make(chan struct{})
		call := residentToolStep(tool, args)
		call.Block = release
		model.set(key, call)
		before := s.Snapshot().CurrentTurn
		if receipt, err := s.Submit(ctx, api.Submission{ID: strings.ToLower(key), Text: key}, files); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		waitForTaskMCP(s)
		close(release)
		waitTurn(t, ctx, s, before)
		t.Logf("%s completed via Core stdio", key)
	}
	const first = "CASE_STDIO_NATIVE_INITIAL"
	const firstID = "native-stdio-start"
	waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
	model.set(attachmentWorkerFixture, modelStep{Name: "js_exec", BuildArgs: func() any {
		paths, _ := filepath.Glob(filepath.Join(opts.Directory, "native-worker-inputs", "*", "0-selected.txt"))
		if len(paths) != 1 {
			return map[string]any{"code": "throw new Error('worker attachment absent');"}
		}
		arg, _ := json.Marshal(map[string]string{"path": paths[0]})
		return map[string]any{"code": "const listing = await tools.search({names:['caelis_tasks__bot_delegate','caelis_tasks__bot_event','caelis_tasks__bot_gesture','caelis_tasks__bot_interactions','caelis_tasks__bot_schedule','caelis_tasks__bot_schedule_update','caelis_tasks__bot_tasks','Read']}); " +
			"if (listing.tools.slice(0,7).some(item => item.error_code !== 'not_found') || !listing.tools[7].entry) throw new Error('unexpected Worker catalog'); " +
			"const file = await tools.call('Read'," + string(arg) + "); return {taskEntries: listing.tools.slice(0,7), file: file.data ?? file.text};"}
	}})
	model.set(first, modelStep{Name: "js_exec", BuildArgs: func() any {
		var attachments []map[string]any
		for _, ref := range uploads.References(firstID) {
			attachments = append(attachments, map[string]any{"id": ref.ID})
		}
		return residentToolStep("bot_delegate", map[string]any{"title": "Synthetic assignment", "prompt": attachmentWorkerFixture + " Return the initial synthetic result.", "attachments": attachments}).Args
	}})
	before := s.Snapshot().CurrentTurn
	if receipt, err := service.Submit(ctx, api.Submission{ID: firstID, Text: first, FileIDs: []string{"selected"}}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("submit with resident attachment", receipt, err)
	}
	waitTurn(t, ctx, s, before)
	initialOutput := fmt.Sprint(dtwLastToolOutput(model.seen(first)))
	if !strings.Contains(initialOutput, `"outcome":"accepted"`) {
		t.Fatal("private MCP did not accept the original delegation", readStatus(s, sid))
	}
	list := manager.ListTasks()
	if len(list) != 1 {
		t.Fatal("private MCP did not create one Worker", list)
	}
	task := list[0].ID
	s.mu.Lock()
	original := s.state.Workers[task]
	s.mu.Unlock()
	var coreSkill bool
	for _, skill := range readStatus(s, sid).Skills {
		coreSkill = coreSkill || value(skill.Name) == "bot-core" && skill.Status == "ready"
	}
	available := readStatus(s, sid)
	if !coreSkill || original.Binding.SessionId == "" || !taskMCPReady(available) {
		t.Fatal("resident native task MCP, Skill or Worker binding missing", available)
	}
	select {
	case call := <-mcpInvocation:
		if !call.CaelisMCP || call.CallID == "" || call.Session != "" || call.Turn != "" || call.TurnKey != "" {
			t.Fatal("stdio delegation attached unproven native provenance", call)
		}
	case <-ctx.Done():
		t.Fatal("native Worker start did not report its original MCP invocation")
	}
	finish := func() {
		t.Helper()
		var token string
		waitAcceptance(t, ctx, func() bool {
			if !slices.ContainsFunc(s.WorkStates(), func(state api.WorkState) bool { return state.Task.ID == task && state.Task.Status == "completed" }) {
				return false
			}
			// A streamed terminal result can precede the native idle read.
			observed, err := manager.ObserveTask(ctx, task)
			if err != nil {
				t.Fatal(err)
			}
			if observed.State.Activity != "idle" || observed.State.Task.Status != "completed" {
				return false
			}
			token = api.TaskFinishToken(observed.State)
			return token != ""
		})
		if _, err := manager.FinishTask(ctx, task, token); err != nil {
			t.Fatal(err)
		}
	}
	finish()
	workerOutput := fmt.Sprint(dtwLastToolOutput(model.seen(attachmentWorkerFixture)))
	if !strings.Contains(workerOutput, strings.TrimSpace(payload)) || strings.Count(workerOutput, "not_found") != 7 {
		t.Fatal("original Worker catalog or native attachment Read differs", workerOutput)
	}
	t.Log("native Worker tool search returned not_found for all seven resident tools and found Read; Read returned the selected attachment bytes")
	for _, server := range readStatus(s, original.Binding.SessionId).Servers {
		if server.Name == privateTasksService {
			t.Fatal("Worker inherited private services")
		}
	}
	for _, skill := range readStatus(s, original.Binding.SessionId).Skills {
		if value(skill.Name) == "bot-core" {
			t.Fatal("Worker inherited bot-core")
		}
	}
	report := "CASE_STDIO_NATIVE_INITIAL"
	model.mu.Lock()
	model.plans[report] = append(make([]modelStep, model.counts[report]), residentToolStep("bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}))
	model.mu.Unlock()
	before = s.Snapshot().CurrentTurn
	if err := manager.DeliverTaskReport(ctx); err != nil {
		t.Fatal(err)
	}
	waitTurn(t, ctx, s, before)
	// Inspect the real read in the report activation. The report can also be
	// discovered from the native journal independently of the model stream.
	read := fmt.Sprint(dtwLastToolOutput(model.seen(report)))
	if !strings.Contains(read, "ACCEPTANCE_COMPLETE") || !strings.Contains(read, `"status":"completed"`) {
		t.Fatal("completion report did not read the original Worker result", read)
	}
	run("CASE_STDIO_NATIVE_FOLLOWUP", "bot_delegate", map[string]any{"task": task, "prompt": "Continue the same Worker with a second result."}, nil)
	finish()
	s.mu.Lock()
	next := s.state.Workers[task]
	state := clone(s.state)
	s.mu.Unlock()
	if next.Binding.SessionId != original.Binding.SessionId || next.PromptID == original.PromptID || len(manager.ListTasks()) != 1 {
		t.Fatal("private follow-up changed Worker owner or lacked the native prompt")
	}
	for _, saved := range state.Calls {
		if strings.HasPrefix(saved.Call.Name, "bot_") || strings.HasPrefix(saved.Call.Name, privateTasksPrefix) {
			t.Fatal("native MCP delegation used an Application callback", saved.Call.ToolsVersion)
		}
	}
	for _, operation := range []string{original.PromptID, next.PromptID} {
		j := state.Operations[operation]
		var receipt wire.CommandResult
		if operation == "" || !succeeded(wire.Outcome(j.Outcome)) ||
			j.Path != "/sessions/"+idPath(original.Binding.SessionId)+"/prompt" ||
			s.client.json(ctx, "GET", "/operations/"+idPath(operation), nil, &receipt, "", "") != nil ||
			receipt.OperationId != operation || !succeeded(receipt.Outcome) {
			t.Fatal("original native Worker receipt is unavailable or changed", operation, j.Outcome, receipt.Outcome)
		}
	}
	followup := fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_FOLLOWUP")))
	if !strings.Contains(followup, `"outcome":"accepted"`) {
		t.Fatal("original Worker continuation was not accepted by Core", followup)
	}
	createID := original.operationID("create", task)
	create := state.Operations[createID]
	body, _ := json.Marshal(wire.CreateSessionRequest{OperationId: &createID, Cwd: &original.Task.Workspace, Title: &original.Task.Title,
		Model: &workerModel, ReasoningEffort: pointer(opts.WorkExecution.Effort), FastMode: pointer(false)})
	if create.Path != "/sessions" || create.Resource != original.Binding.SessionId || !succeeded(wire.Outcome(create.Outcome)) ||
		create.Digest != digest(body) {
		t.Fatal("native Worker create receipt or request differs from owned binding", createID, create.Outcome)
	}
	t.Logf("native Worker operation %s %s %s", createID, create.Path, create.Outcome)
	for _, id := range []string{original.PromptID, next.PromptID} {
		op := state.Operations[id]
		if op.Path != "/sessions/"+idPath(next.Binding.SessionId)+"/prompt" || !succeeded(wire.Outcome(op.Outcome)) {
			t.Fatal("native Worker receipt missing", id, op.Path, op.Outcome)
		}
		t.Logf("native Worker operation %s %s %s", id, op.Path, op.Outcome)
	}
	probe := "native-worker-resource-synthetic"
	workerSID := original.Binding.SessionId
	path := "/sessions/" + idPath(workerSID) + "/resources"
	request := wire.SessionResourceRequest{OperationId: &probe, SessionId: &workerSID, Name: "selected.txt", MediaType: "text/plain",
		Data: base64.StdEncoding.EncodeToString([]byte(payload)), Sha256: digest([]byte(payload))}
	response, err := s.client.request(ctx, "POST", path, request, probe, "")
	if err != nil {
		t.Fatal("capture native resource route refusal", err)
	}
	var resource map[string]any
	err = json.NewDecoder(response.Body).Decode(&resource)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 404 || resource["session_resource"] != nil {
		t.Fatal("unexpected native resource probe", response.StatusCode, resource, err)
	}
	encoded, _ := json.Marshal(resource)
	t.Logf("POST /api/control/v1%s with operation_id=%s session_id=%s name=selected.txt media_type=text/plain data=%s sha256=%s returned HTTP %d %s",
		path, probe, workerSID, request.Data, request.Sha256, response.StatusCode, encoded)

	bridge.Close()
	resident.Close()
	if err := s.Close(ctx); err != nil {
		t.Fatal("close original adapter", err)
	}
	s = New(opts)
	resident, err = bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	manager, err = tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", &reportLoopRuntime{Session: s}, resident, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := resident.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	restartedBridge, err := bot.Serve(resident)
	if err != nil {
		t.Fatal(err)
	}
	defer restartedBridge.Close()
	cfg.PublishCaelisMCP = restartedBridge.Config(executable).PublishCaelisMCP
	if err := s.ConfigureBotTools(cfg); err != nil {
		t.Fatal("republish the Notebook bridge after restart", err)
	}
	if err := s.Connect(ctx); err != nil {
		t.Fatal("restore original resident binding", err)
	}
	resident.Start(s)
	if s.ConversationState().Session != sid {
		t.Fatal("resident reconnect replaced the original Session")
	}
	run("CASE_STDIO_NATIVE_RESUMED", "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}, nil)
	if !strings.Contains(fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_RESUMED"))), "Continue the same Worker") {
		t.Fatal("original Worker history lost on resident reconnect")
	}
	remote, _ := nativeMCPDirectoryFixture(t)
	defer remote.Close()
	selection := plugins.Selection{
		Servers: []plugins.SelectedServer{
			{PackageID: "fixture", Name: "first", Server: plugins.Server{Type: "streamable-http", URL: remote.URL + "/first"}},
			{PackageID: "fixture", Name: "second", Server: plugins.Server{Type: "streamable-http", URL: remote.URL + "/second"}},
		},
	}
	if err := s.UpdateBotPlugins(ctx, selection); err != nil {
		t.Fatal("add managed services beside private MCP", err)
	}
	assertServers := func(in plugins.Selection) {
		t.Helper()
		current, err := s.Configuration(ctx)
		if err != nil || !hasNativeTaskMCP(current.Profile) || len(current.Profile.McpServers) != len(in.Servers)+1 {
			t.Fatal("service refresh changed the native resident catalog", err)
		}
		for _, item := range in.Servers {
			if !hasNativeMCPServer(current.Profile.McpServers, plugins.RuntimeName(item.PackageID, item.Name)) {
				t.Fatal("missing managed service", item.Name)
			}
		}
	}
	assertServers(selection)
	selection.Servers = selection.Servers[:1]
	if err := s.UpdateBotPlugins(ctx, selection); err != nil {
		t.Fatal("remove one managed service", err)
	}
	assertServers(selection)
	run("CASE_STDIO_NATIVE_AFTER_REFRESH", "bot_tasks", map[string]any{"request": map[string]any{"type": "list"}}, nil)
	if !strings.Contains(fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_AFTER_REFRESH"))), task) {
		t.Fatal("refreshed resident lost the private task service")
	}
	t.Log("real Session added two managed MCP servers and removed one while retaining the native task service")

	review(`{"option_id":"cancel","rationale":"This synthetic assignment is not allowed."}`, nil)
	run("CASE_STDIO_NATIVE_DENIED", "bot_delegate", map[string]any{"prompt": "A denied synthetic delegation."}, nil)
	if len(manager.ListTasks()) != 1 {
		t.Fatal("denied review created a Worker")
	}
	waitAcceptance(t, ctx, func() bool {
		return slices.ContainsFunc(s.Snapshot().Reviews, func(review api.Review) bool {
			return review.Status == "denied" && (strings.Contains(review.Action, "A denied synthetic delegation") ||
				strings.Contains(review.Action, privateTasksPrefix+"bot_delegate"))
		})
	})
	review(`{"option_id":"allow_once"}`, nil)
	run("CASE_STDIO_NATIVE_SCHEDULE_CONTEXT", "bot_schedule", map[string]any{"request": map[string]any{"type": "context"}}, nil)
	run("CASE_STDIO_NATIVE_GESTURE", "bot_gesture", map[string]any{"action": "nod"}, nil)
	run("CASE_STDIO_NATIVE_EVENT", "bot_event", map[string]any{"request": map[string]any{
		"type": "task", "id": "original-worker-event", "label": "Observe the fixture Worker", "task": task,
	}}, nil)
	if out := fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_EVENT"))); !strings.Contains(out, "event:original-worker-event") {
		t.Fatal("approved event registration did not persist the original task observation")
	}
	review(`{"option_id":"allow_once"}`, nil)
	questions := &residentQuestionFixture{manager: manager, question: api.WorkerQuestion{
		ID: "Q7", Binding: "fixture-original-binding", TaskID: task,
		Title: "Which fixture option?", Options: []string{"Fixture option A"},
	}}
	resident.ConfigureWorkerInteractions(questions)
	run("CASE_STDIO_NATIVE_QUESTION", "bot_interactions", map[string]any{"request": map[string]any{"type": "list"}}, nil)
	if out := fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_QUESTION"))); !strings.Contains(out, questions.question.ID) {
		t.Fatal("MCP result did not include the original fixture question")
	}
	run("CASE_STDIO_NATIVE_ANSWER", "bot_interactions", map[string]any{"request": map[string]any{
		"type": "answer", "question": questions.question.ID, "binding": questions.question.Binding,
		"value": "Fixture option A", "requestId": "answer-original-stdio-fixture",
	}}, nil)
	finish()
	run("CASE_STDIO_NATIVE_ANSWER_RESULT", "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}, nil)
	answered := fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_ANSWER_RESULT")))
	s.mu.Lock()
	answeredWorker := s.state.Workers[task]
	answeredReceipt := s.state.Operations[answeredWorker.operationID("send", "answer-original-stdio-fixture")]
	s.mu.Unlock()
	if questions.replies.Load() != 1 || answeredWorker.Binding.SessionId != original.Binding.SessionId ||
		!succeeded(wire.Outcome(answeredReceipt.Outcome)) || !strings.Contains(answered, "Worker answer fixture: Fixture option A") {
		t.Fatal("reviewed MCP answer did not continue the original Worker and retrieve its result")
	}
	t.Log("fixture question ID/binding crossed real reviewed stdio; native Core committed the original Worker continuation and returned its result")
	runNativeReminderAcceptance(t, ctx, s, resident, model, review)
	run("CASE_STDIO_NATIVE_AFTER_IDLE", "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}, nil)
	if !strings.Contains(fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_AFTER_IDLE"))), "Continue the same Worker") {
		t.Fatal("idle resident rotation lost the original native Worker")
	}
	t.Log("switching to retained manual approval Session")
	manualOpts := opts
	manualOpts.Directory, manualOpts.RequireApproval = filepath.Join(root, "manual"), true
	manualNotebook := filepath.Join(root, "ManualNotebook")
	if err := os.Mkdir(manualNotebook, 0700); err != nil {
		t.Fatal(err)
	}
	manual := New(manualOpts)
	defer manual.Close(context.Background())
	legacyManual := legacy.Clone()
	legacyManual.NotebookDirectory = manualNotebook
	if err := manual.ConfigureBotTools(legacyManual); err != nil {
		t.Fatal(err)
	}
	if err := manual.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := manual.Close(ctx); err != nil {
		t.Fatal(err)
	}
	resumedManual := New(manualOpts)
	defer resumedManual.Close(context.Background())
	manualResident, err := bot.NewForRuntime(filepath.Join(root, "manual-bot", "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	manualManager, err := tasks.Open(filepath.Join(root, "manual-bot", "tasks.json"), filepath.Join(root, "manual-tasks"), "caelis", resumedManual, manualResident, resumedManual.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := manualResident.ConfigureTasks(manualManager, nil); err != nil {
		t.Fatal(err)
	}
	defer manualResident.Close()
	manualBridge, err := bot.Serve(manualResident)
	if err != nil {
		t.Fatal(err)
	}
	defer manualBridge.Close()
	manualConnection := manualBridge.Config(executable)
	manualConnection.Args = cfg.Args
	manualConnection.NotebookDirectory = manualNotebook
	manualConnection.Instructions, manualConnection.BuiltinSkillRoots = cfg.Instructions, cfg.BuiltinSkillRoots
	if err := resumedManual.ConfigureBotTools(manualConnection); err != nil {
		t.Fatal(err)
	}
	if err := resumedManual.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	manualResident.Start(resumedManual)
	manualConfig, err := resumedManual.Configuration(ctx)
	if err != nil || !hasNativeTaskMCP(manualConfig.Profile) {
		t.Fatal("retained manual Session did not migrate to native MCP", err)
	}
	waitAcceptance(t, ctx, func() bool { return resumedManual.Snapshot().CanSend })
	submitAcceptance(t, ctx, resumedManual, "CASE_STDIO_MANUAL_READY")
	waitForTaskMCP(resumedManual)
	t.Log("manual resident MCP discovered")
	for _, test := range []struct{ key, decision string }{{"CASE_NATIVE_MANUAL_ALLOW", "allow_once"}, {"CASE_NATIVE_MANUAL_DENY", "cancel"}} {
		waitAcceptance(t, ctx, func() bool { return resumedManual.Snapshot().CanSend })
		model.set(test.key, residentToolStep("bot_delegate", map[string]any{"prompt": "Return a synthetic manual-decision Worker result."}))
		before := resumedManual.Snapshot().CurrentTurn
		if out, err := resumedManual.Submit(ctx, api.Submission{ID: strings.ToLower(test.key), Text: test.key}, nil); err != nil || out.Outcome != "accepted" {
			t.Fatal(out, err)
		}
		var approval string
		manualReady, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		waitAcceptance(t, manualReady, func() bool {
			if manualReady.Err() != nil {
				t.Log("manual fixture stage", test.key, "model requests", len(model.seen(test.key)), "permissions pending", len(resumedManual.Snapshot().Approvals))
			}
			for _, item := range resumedManual.Snapshot().Approvals {
				if slices.ContainsFunc(item.Choices, func(choice api.Choice) bool { return choice.ID == test.decision }) {
					approval = item.ID
					return true
				}
			}
			return false
		})
		if err := resumedManual.Decide(ctx, api.Decision{ID: approval, Choice: test.decision}); err != nil {
			t.Fatal(err)
		}
		waitTurn(t, ctx, resumedManual, before)
		result := fmt.Sprint(dtwLastToolOutput(model.seen(test.key)))
		resumedManual.mu.Lock()
		decision := resumedManual.state.Operations["approval-"+approval]
		resumedManual.mu.Unlock()
		allow := test.decision == "allow_once"
		if strings.Contains(result, `"ok":true`) != allow || len(manualManager.ListTasks()) != 1 ||
			decision.ManualMCP != nil || decision.Outcome != "committed" {
			t.Fatal("manual native decision unexpectedly dispatched or refused the tool", test.decision, result)
		}
		t.Logf("retained manual Session used native approval %s", test.decision)
	}
	const cancelled = "CASE_NATIVE_MANUAL_CANCEL"
	model.set(cancelled, residentToolStep("bot_delegate", map[string]any{"prompt": "Return a synthetic interrupted Worker result."}))
	if out, err := resumedManual.Submit(ctx, api.Submission{ID: strings.ToLower(cancelled), Text: cancelled}, nil); err != nil || out.Outcome != "accepted" {
		t.Fatal(out, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(resumedManual.Snapshot().Approvals) > 0 })
	pendingHead := resumedManual.ConversationState()
	if err := resumedManual.RenewIdleConversation(ctx, "pending-approval-renewal", pendingHead); err == nil ||
		resumedManual.ConversationState().Session != pendingHead.Session || len(resumedManual.Snapshot().Approvals) != 1 {
		t.Fatal("idle rotation displaced a live native approval", err)
	}
	if err := resumedManual.Interrupt(ctx); err != nil {
		t.Fatal("cancel the turn while its native approval is pending", err)
	}
	waitAcceptance(t, ctx, func() bool {
		view := resumedManual.Snapshot()
		return view.CanSend && len(view.Approvals) == 0
	})
	if len(manualManager.ListTasks()) != 1 {
		t.Fatal("cancelling a pending approval created a Worker")
	}
	t.Log("native turn cancellation closed the pending approval without creating a Worker")
	retired := resumedManual.ConversationState()
	if err := resumedManual.RenewIdleConversation(ctx, "retire-manual-resident", retired); err != nil {
		t.Fatal("rotate settled manual resident", err)
	}
	// An explicit Core client can still prompt its own historical Session.
	// That decision belongs to the original Core client, outside Bot routing.
	const restored = "CASE_NATIVE_MANUAL_RESTORED"
	ready := make(chan struct{})
	step := residentToolStep("bot_delegate", map[string]any{"prompt": "Synthetic historical approval."})
	step.Block = ready
	model.set(restored, step)
	retiredID, op := retired.Session, "explicit-core-history-prompt"
	retiredPath := "/sessions/" + idPath(retiredID)
	var outcome wire.CommandResult
	if err := resumedManual.client.json(ctx, "POST", retiredPath+"/prompt", wire.PromptRequest{OperationId: &op, SessionId: &retiredID, Input: pointer(restored)}, &outcome, op, ""); err != nil ||
		!succeeded(outcome.Outcome) {
		close(ready)
		t.Fatal("explicit Core history fixture was not admitted", err)
	}
	waitAcceptance(t, ctx, func() bool { return taskMCPReady(readStatus(resumedManual, retiredID)) })
	close(ready)
	var head wire.SessionState
	waitAcceptance(t, ctx, func() bool {
		return resumedManual.client.json(ctx, "GET", retiredPath+"/state", nil, &head, "", "") == nil && head.Approval.Active != nil
	})
	if err := resumedManual.refreshMain(ctx, resumedManual.client, retiredID); err != nil {
		t.Fatal(err)
	}
	resumedManual.mu.Lock()
	oldApproval := approvalID(resumedManual.state.InstanceID, retiredID, head.Approval.Active)
	resumedManual.mu.Unlock()
	var invalidDecision *api.DecisionValidationError
	if err := resumedManual.Decide(ctx, api.Decision{ID: oldApproval, Choice: "allow_once"}); len(resumedManual.Snapshot().Approvals) != 0 ||
		!errors.As(err, &invalidDecision) {
		t.Fatal("Bot offered or resolved a previous Session's approval", err)
	}
	var originalApproval wire.SessionState
	if err := resumedManual.client.json(ctx, "GET", retiredPath+"/state", nil, &originalApproval, "", ""); err != nil ||
		originalApproval.Approval.Active == nil || originalApproval.Approval.Active.RequestId != head.Approval.Active.RequestId ||
		len(manualManager.ListTasks()) != 1 {
		t.Fatal("Bot changed the old Core approval or created a Worker", err)
	}
	t.Log("current approval deferred rotation; an explicitly restored previous Session remained visible to Core with its own pending approval and no Bot action")
}

func runNativeReminderAcceptance(t *testing.T, ctx context.Context, s *Session, resident *bot.Runtime, model *acceptanceModel, review func(string, <-chan struct{})) {
	t.Helper()
	review(`{"option_id":"allow_once","rationale":"Allow this bounded synthetic reminder."}`, nil)
	fireAt, revokeAt := time.Now().Add(7*time.Second).Truncate(time.Second), time.Now().Add(11*time.Second).Truncate(time.Second)
	schedule := func(id, prompt string, at time.Time) modelStep {
		return residentToolStep("bot_schedule_update", map[string]any{"request": map[string]any{
			"type": "save", "id": id, "label": id, "prompt": prompt,
			"trigger": map[string]any{"type": "calendar", "timeZone": "UTC", "schedule": map[string]any{"type": "at", "at": at.UTC().Format(time.RFC3339)}},
		}})
	}
	run := func(key string, step modelStep) {
		t.Helper()
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
		before := s.Snapshot().CurrentTurn
		model.set(key, step)
		notice := api.SystemMessage{ID: strings.ToLower(key), Text: key, Source: api.SystemWorkerResult}
		if out, err := resident.SubmitSystem(ctx, notice); err != nil || out.Outcome != "accepted" {
			t.Fatal("registry input was not accepted as Worker material", out, err)
		}
		waitTurn(t, ctx, s, before)
	}
	run("CASE_NATIVE_REMINDER_REGISTER", schedule("native-fire", "synthetic reminder should fire", fireAt))
	run("CASE_NATIVE_REMINDER_CANCEL_REGISTER", schedule("native-revoke", "synthetic revoked reminder must stay silent", revokeAt))
	if !slices.ContainsFunc(resident.State().Schedules, func(item bot.Schedule) bool { return item.ID == "native-revoke" && item.Enabled }) {
		t.Fatal("native MCP did not register the revocable reminder")
	}
	run("CASE_NATIVE_REMINDER_REVOKE", residentToolStep("bot_schedule_update", map[string]any{
		"request": map[string]any{"type": "remove", "automation": "calendar:native-revoke"},
	}))
	if slices.ContainsFunc(resident.State().Schedules, func(item bot.Schedule) bool { return item.ID == "native-revoke" }) {
		t.Fatal("reviewed wake removal did not reach stored arrangement")
	}
	head := s.ConversationState()
	if err := s.RenewIdleConversation(ctx, "reminder-boundary-idle", head); err != nil {
		t.Fatal("renew resident with registered reminders", err)
	}
	replacement := s.ConversationState()
	if replacement.Session == head.Session {
		t.Fatal("idle resident retained its original session")
	}
	observedWakes := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		count := 0
		for id, operation := range s.state.Operations {
			if strings.HasPrefix(id, "wake-") && operation.Outcome == "committed" && operation.Source.Kind == "external_material" {
				if operation.Path != "/sessions/"+idPath(replacement.Session)+"/prompt" || len(operation.OriginRequests) != 0 {
					t.Errorf("post-rotation reminder acquired an old Session or an unproven input route: %s", operation.Path)
				}
				count++
			}
		}
		return count
	}
	wakeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	for {
		if wake := resident.State().Wake; wake != nil && wake.Status == "accepted" &&
			slices.Equal(wake.ScheduleIDs, []string{"native-fire"}) && observedWakes() == 1 {
			waitAcceptance(t, wakeCtx, func() bool { return s.BackgroundResult(wake.ID).Complete })
			break
		}
		select {
		case <-wakeCtx.Done():
			s.mu.Lock()
			for id, pending := range s.state.Operations {
				if strings.HasPrefix(id, "wake-") {
					t.Log("native wake", id, pending.Path, pending.Outcome, string(pending.Body))
				}
			}
			s.mu.Unlock()
			t.Fatal("native reminder did not commit", resident.State().Wake, resident.State().Schedules)
		case <-time.After(100 * time.Millisecond):
		}
	}
	waitAcceptance(t, ctx, func() bool { return time.Now().After(revokeAt.Add(1200 * time.Millisecond)) })
	if count := observedWakes(); count != 1 {
		t.Fatal("unexpected native scheduler execution", count)
	}
	var old wire.SessionState
	if err := s.client.json(ctx, "GET", "/sessions/"+idPath(head.Session)+"/state", nil, &old, "", ""); err != nil ||
		nativeRunActive(old) || value(old.Run.TurnId) != "" && value(old.Run.TurnId) != head.Turn {
		t.Fatal("scheduled activity changed the original resident", err)
	}
	t.Log("real Core registered from worker_result, dispatched one native external_material reminder and suppressed the revoked reminder after its due time")
}

func sortedTools(names []string) []string {
	names = slices.Clone(names)
	slices.Sort(names)
	return names
}

func residentToolStep(tool string, args map[string]any) modelStep {
	return nativeMCPStep(privateTasksPrefix+tool, args)
}

func nativeMCPStep(tool string, args any) modelStep {
	name, _ := json.Marshal(tool)
	raw, _ := json.Marshal(args)
	return modelStep{Name: "js_exec", Args: map[string]any{"code": "await tools.search({name:" + string(name) + "}); return await tools.call(" + string(name) + "," + string(raw) + ");"}}
}

func hasNativeTaskMCP(profile *wire.SessionProfile) bool {
	return profile != nil && len(profile.Tools) == 0 && hasNativeMCPServer(profile.McpServers, privateTasksService)
}
