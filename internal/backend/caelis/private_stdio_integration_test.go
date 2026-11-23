package caelis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func privateStdioWorkerRoundTrip(t *testing.T, settings api.RuntimeSettings, mainModel, workerModel string, model *acceptanceModel, review func(string, <-chan struct{})) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 72*time.Second)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skill, err := botskills.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Directory: filepath.Join(root, "binding"), Settings: settings, Execution: api.ExecutionSettings{Model: mainModel}, WorkExecution: api.WorkExecutionSettings{Model: workerModel}}
	resident, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resident.Close()
	old := New(opts)
	defer old.Close(context.Background())
	legacy := &api.ToolConnection{Host: resident, Instructions: botskills.Bootstrap(), BuiltinSkillRoots: []string{filepath.Dir(skill)}}
	if err := old.ConfigureBotTools(legacy); err != nil {
		t.Fatal(err)
	}
	if err := old.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	waitAcceptance(t, ctx, func() bool { return old.Snapshot().CanSend })
	submitAcceptance(t, ctx, old, "CASE_STDIO_LEGACY_RESIDENT")
	oldID := old.ConversationState().Session
	if err := old.Close(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(opts)
	defer func() { _ = s.Close(context.Background()) }()
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", &reportLoopRuntime{Session: s}, resident, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil || !hasResidentMCPAliases(current.Profile) {
		t.Fatal("resident did not migrate to the authenticated callback catalog", err)
	}
	readStatus := func(sid string) wire.SessionMCPStatus {
		t.Helper()
		var status wire.SessionMCPStatus
		if err := s.client.json(ctx, "GET", "/sessions/"+idPath(sid)+"/mcp-status", nil, &status, "", ""); err != nil {
			t.Fatal(err)
		}
		return status
	}
	step := func(tool string, args map[string]any) modelStep {
		name, _ := json.Marshal(privateTasksService + "__" + tool)
		raw, _ := json.Marshal(args)
		return modelStep{Name: "js_exec", Args: map[string]any{"code": "await tools.search({name:" + string(name) + "}); return await tools.call(" + string(name) + "," + string(raw) + ");"}}
	}
	run := func(key, tool string, args map[string]any, files []api.InputFile) {
		t.Helper()
		waitAcceptance(t, ctx, func() bool { return s.Snapshot().CanSend })
		model.set(key, step(tool, args))
		before := s.Snapshot().CurrentTurn
		if receipt, err := s.Submit(ctx, api.Submission{ID: strings.ToLower(key), Text: key}, files); err != nil || receipt.Outcome != "accepted" {
			t.Fatal(receipt, err)
		}
		waitTurn(t, ctx, s, before)
		t.Logf("%s native result %s", key, dtwLastToolOutput(model.seen(key)))
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
		return map[string]any{"code": "await tools.search({name:'Read'}); return await tools.call('Read'," + string(arg) + ");"}
	}})
	model.set(first, modelStep{Name: "js_exec", BuildArgs: func() any {
		var attachments []map[string]any
		for _, ref := range uploads.References(firstID) {
			attachments = append(attachments, map[string]any{"id": ref.ID})
		}
		return step("bot_delegate", map[string]any{"title": "Synthetic assignment", "prompt": attachmentWorkerFixture + " Return the initial synthetic result.", "attachments": attachments}).Args
	}})
	before := s.Snapshot().CurrentTurn
	if receipt, err := service.Submit(ctx, api.Submission{ID: firstID, Text: first, FileIDs: []string{"selected"}}); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("submit with resident attachment", receipt, err)
	}
	waitTurn(t, ctx, s, before)
	initialOutput := fmt.Sprint(dtwLastToolOutput(model.seen(first)))
	t.Logf("native initial result %s", initialOutput)
	if !strings.Contains(initialOutput, `"outcome":"accepted"`) {
		t.Fatal("private MCP did not accept the original delegation")
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
	for _, skill := range readStatus(sid).Skills {
		coreSkill = coreSkill || value(skill.Name) == "bot-core" && skill.Status == "ready"
	}
	if !coreSkill || original.Binding.SessionId == "" {
		t.Fatal("resident Skill projection or Worker binding missing")
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
			token = api.TaskFinishToken(observed.State)
			return token != ""
		})
		if _, err := manager.FinishTask(ctx, task, token); err != nil {
			t.Fatal(err)
		}
	}
	finish()
	workerOutput := fmt.Sprint(dtwLastToolOutput(model.seen(attachmentWorkerFixture)))
	if !strings.Contains(workerOutput, strings.TrimSpace(payload)) {
		t.Fatal("original Worker did not read the selected attachment through its native tool", workerOutput)
	}
	t.Log("native Worker Read returned the selected attachment bytes")
	for _, server := range readStatus(original.Binding.SessionId).Servers {
		if server.Name == privateTasksService {
			t.Fatal("Worker inherited private services")
		}
	}
	for _, skill := range readStatus(original.Binding.SessionId).Skills {
		if value(skill.Name) == "bot-core" {
			t.Fatal("Worker inherited bot-core")
		}
	}
	report := "CASE_STDIO_NATIVE_INITIAL"
	model.mu.Lock()
	model.plans[report] = append(make([]modelStep, model.counts[report]), step("bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}))
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
	t.Logf("native report result %s", read)
	run("CASE_STDIO_NATIVE_FOLLOWUP", "bot_delegate", map[string]any{"task": task, "prompt": "Continue the same Worker with a second result."}, nil)
	finish()
	s.mu.Lock()
	next := s.state.Workers[task]
	state := clone(s.state)
	s.mu.Unlock()
	if next.Binding.SessionId != original.Binding.SessionId || next.PromptID == original.PromptID || len(manager.ListTasks()) != 1 {
		t.Fatal("private follow-up changed Worker owner or lacked the native prompt")
	}
	var delegateCalls int
	for _, saved := range state.Calls {
		if saved.Call.Name == privateTasksPrefix+"bot_delegate" && saved.Call.SessionId == sid &&
			saved.Call.PrincipalId == state.PrincipalID && saved.Receipt != nil && saved.Receipt.Outcome == "succeeded" {
			delegateCalls++
		}
	}
	if delegateCalls != 2 {
		t.Fatal("Worker effects did not retain their two native resident callback receipts", delegateCalls)
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

	if err := s.Reconnect(ctx); err != nil {
		t.Fatal("reconnect original resident", err)
	}
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
		if err != nil || !hasResidentMCPAliases(current.Profile) || len(current.Profile.McpServers) != len(in.Servers) {
			t.Fatal("service refresh changed the authenticated resident catalog", err)
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
	t.Log("real Session added two managed MCP servers and removed one while retaining the authenticated task catalog")

	review(`{"option_id":"reject_once","rationale":"This synthetic assignment is not allowed."}`, nil)
	run("CASE_STDIO_NATIVE_DENIED", "bot_delegate", map[string]any{"prompt": "A denied synthetic delegation."}, nil)
	if len(manager.ListTasks()) != 1 {
		t.Fatal("denied review created a Worker")
	}
	waitAcceptance(t, ctx, func() bool {
		return slices.ContainsFunc(s.Snapshot().Reviews, func(review api.Review) bool {
			return review.Status == "denied" && strings.Contains(review.Action, "A denied synthetic delegation")
		})
	})
	runNativeReminderAcceptance(t, ctx, s, resident, model, review)
	run("CASE_STDIO_NATIVE_AFTER_IDLE", "bot_tasks", map[string]any{"request": map[string]any{"type": "read", "task": task}}, nil)
	if !strings.Contains(fmt.Sprint(dtwLastToolOutput(model.seen("CASE_STDIO_NATIVE_AFTER_IDLE"))), "Continue the same Worker") {
		t.Fatal("idle resident rotation lost the original native Worker")
	}
	manualOpts := opts
	manualOpts.Directory, manualOpts.RequireApproval = filepath.Join(root, "manual"), true
	manual := New(manualOpts)
	defer manual.Close(context.Background())
	if err := manual.ConfigureBotTools(legacy); err != nil {
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
	manualConnection.Instructions, manualConnection.BuiltinSkillRoots = cfg.Instructions, cfg.BuiltinSkillRoots
	if err := resumedManual.ConfigureBotTools(manualConnection); err != nil {
		t.Fatal(err)
	}
	if err := resumedManual.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	manualResident.Start(resumedManual)
	manualConfig, err := resumedManual.Configuration(ctx)
	if err != nil || !hasResidentMCPAliases(manualConfig.Profile) {
		t.Fatal("retained manual Session did not migrate to the authenticated callback catalog", err)
	}
	for _, test := range []struct{ key, decision string }{{"CASE_NATIVE_MANUAL_ALLOW", "allow_once"}, {"CASE_NATIVE_MANUAL_DENY", "reject_once"}} {
		waitAcceptance(t, ctx, func() bool { return resumedManual.Snapshot().CanSend })
		model.set(test.key, step("bot_delegate", map[string]any{"prompt": "Return a synthetic manual-decision Worker result."}))
		before := resumedManual.Snapshot().CurrentTurn
		if out, err := resumedManual.Submit(ctx, api.Submission{ID: strings.ToLower(test.key), Text: test.key}, nil); err != nil || out.Outcome != "accepted" {
			t.Fatal(out, err)
		}
		var approval string
		waitAcceptance(t, ctx, func() bool {
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
			(decision.ManualMCP != nil) != allow || decision.Outcome != "committed" {
			t.Fatal("manual native decision unexpectedly dispatched or refused the tool", test.decision, result)
		}
		t.Logf("retained manual Session used native approval %s", test.decision)
	}
	const cancelled = "CASE_NATIVE_MANUAL_CANCEL"
	model.set(cancelled, step("bot_delegate", map[string]any{"prompt": "Return a synthetic interrupted Worker result."}))
	if out, err := resumedManual.Submit(ctx, api.Submission{ID: strings.ToLower(cancelled), Text: cancelled}, nil); err != nil || out.Outcome != "accepted" {
		t.Fatal(out, err)
	}
	waitAcceptance(t, ctx, func() bool { return len(resumedManual.Snapshot().Approvals) > 0 })
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
}

func runNativeReminderAcceptance(t *testing.T, ctx context.Context, s *Session, resident *bot.Runtime, model *acceptanceModel, review func(string, <-chan struct{})) {
	t.Helper()
	review(`{"option_id":"allow_once","rationale":"Allow this bounded synthetic reminder."}`, nil)
	fireAt, revokeAt := time.Now().Add(7*time.Second).Truncate(time.Second), time.Now().Add(11*time.Second).Truncate(time.Second)
	schedule := func(id, prompt string, at time.Time) modelStep {
		return modelStep{Name: "bot_schedule_update", Args: map[string]any{"request": map[string]any{
			"type": "save", "id": id, "label": id, "prompt": prompt,
			"trigger": map[string]any{"type": "calendar", "timeZone": "UTC", "schedule": map[string]any{"type": "at", "at": at.UTC().Format(time.RFC3339)}},
		}}}
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
		t.Fatal("native callback did not register the revocable reminder")
	}
	run("CASE_NATIVE_REMINDER_REVOKE", modelStep{Name: "bot_schedule_update", Args: map[string]any{
		"request": map[string]any{"type": "remove", "automation": "calendar:native-revoke"},
	}})
	if slices.ContainsFunc(resident.State().Schedules, func(item bot.Schedule) bool { return item.ID == "native-revoke" }) {
		t.Fatal("reviewed wake removal did not reach stored arrangement")
	}
	head := s.ConversationState()
	if err := s.RenewIdleConversation(ctx, "reminder-boundary-idle", head); err != nil {
		t.Fatal("renew resident with registered reminders", err)
	}
	if s.ConversationState().Session == head.Session {
		t.Fatal("idle resident retained its original session")
	}
	observedWakes := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		count := 0
		for id, operation := range s.state.Operations {
			if strings.HasPrefix(id, "wake-") && operation.Outcome == "committed" && operation.Source.Kind == "external_material" {
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
	t.Log("real Core registered from worker_result, dispatched one native external_material reminder and suppressed the revoked reminder after its due time")
}

func hasResidentMCPAliases(profile *wire.SessionProfile) bool {
	if profile == nil || hasNativeMCPServer(profile.McpServers, privateTasksService) {
		return false
	}
	for _, name := range privateTasksTools {
		if slices.ContainsFunc(profile.Tools, func(tool wire.SessionToolDefinition) bool { return tool.Name == name }) ||
			!slices.ContainsFunc(profile.Tools, func(tool wire.SessionToolDefinition) bool { return tool.Name == privateTasksPrefix+name }) {
			return false
		}
	}
	return true
}
