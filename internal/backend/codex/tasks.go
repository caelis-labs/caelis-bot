package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Task records and submission receipts share the atomic conversation binding.
// A recorded unknown outcome is never permission to retry a mutation.
type taskRecord struct {
	OriginalPrompt string                     `json:"originalPrompt,omitempty"`
	Execution      *api.WorkExecutionSettings `json:"execution,omitempty"`
	ModelProvider  string                     `json:"modelProvider,omitempty"`
	View           api.Task                   `json:"view"`
	Thread         string                     `json:"thread"`
	Run            string                     `json:"run"`
	// Native turn IDs displaced by a later, durably recorded continuation. Keep
	// these across reconnects: an old terminal event is not the new result.
	SupersededRuns []string               `json:"supersededRuns,omitempty"`
	Fingerprint    string                 `json:"fingerprint"`
	Source         string                 `json:"source"`
	Requests       map[string]taskReceipt `json:"requests"`
	Pending        string                 `json:"pending,omitempty"`
	ReportID       string                 `json:"reportId,omitempty"`
	ReportState    string                 `json:"reportState,omitempty"`
	SuppressReport bool                   `json:"suppressReport,omitempty"`
	Instructions   string                 `json:"instructions,omitempty"`
}
type taskReceipt struct {
	Fingerprint     string `json:"fingerprint"`
	Outcome         string `json:"outcome"`
	PriorStatus     string `json:"priorStatus,omitempty"`
	PriorRun        string `json:"priorRun,omitempty"`
	PriorResult     string `json:"priorResult,omitempty"`
	PriorSuppressed bool   `json:"priorSuppressed,omitempty"`
	Phase           string `json:"phase,omitempty"`
}

func (s *Session) WorkMessageRecorded(in api.TaskMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.binding.Tasks[in.ID]
	if t == nil {
		return false
	}
	_, ok := t.Requests[in.RequestID]
	return ok
}

func (s *Session) workerParams(workspace, instructions string, t *taskRecord) map[string]any {
	// Codex validates transport even for disabled MCP servers. Supply an inert
	// stdio transport, never the secretary's endpoint/token or approved tool list.
	params := map[string]any{"cwd": workspace, "runtimeWorkspaceRoots": []string{workspace}, "developerInstructions": instructions, "config": map[string]any{
		"mcp_servers.caelis_bot":      map[string]any{"command": os.Args[0], "enabled": false},
		"mcp_servers.caelis_context":  map[string]any{"command": os.Args[0], "enabled": false},
		"mcp_servers.caelis_tasks":    map[string]any{"command": os.Args[0], "enabled": false},
		"mcp_servers.caelis_schedule": map[string]any{"command": os.Args[0], "enabled": false},
		"mcp_servers.caelis_personal": map[string]any{"command": os.Args[0], "enabled": false},
		"mcp_servers.caelis_desktop":  map[string]any{"command": os.Args[0], "enabled": false},
		"agents.enabled":              false,
	}}
	if s.opts.BotTools != nil {
		for _, server := range s.opts.BotTools.Plugins.Servers {
			params["config"].(map[string]any)["mcp_servers."+plugins.RuntimeName(server.PackageID, server.Name)] = map[string]any{"command": os.Args[0], "enabled": false}
		}
	}
	s.applyWorkExecution(params, true, t)
	return params
}

func (s *Session) taskByThread(id string) *taskRecord {
	for _, t := range s.binding.Tasks {
		if t != nil && t.Thread == id && id != "" {
			return t
		}
	}
	return nil
}
func (s *Session) hasBlockingChildren() bool {
	for id := range s.childRuns {
		if s.taskByThread(id) == nil && !s.childObservationFailed[id] {
			return true
		}
	}
	return false
}
func (s *Session) hasUnresolvedTasks() bool {
	for _, task := range s.binding.Tasks {
		if task != nil && (task.Pending != "" || !terminal(task.View.Status)) {
			return true
		}
	}
	return false
}
func (s *Session) hasConversationPrompt() bool {
	for _, p := range s.prompts {
		if p.thread == s.binding.ThreadID || s.taskByThread(p.thread) == nil {
			return true
		}
	}
	return false
}
func (s *Session) WorkStates() []api.WorkState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.WorkState{}
	for _, t := range s.binding.Tasks {
		v := s.taskView(t)
		out = append(out, api.WorkState{Task: v, OriginalPrompt: t.OriginalPrompt, ExecutionKey: t.ReportID, StopRequested: t.SuppressReport, PreviousReportID: t.ReportID, PreviousReportState: t.ReportState, StartFingerprint: t.Fingerprint})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Task.ID < out[j].Task.ID })
	return out
}
func taskRequestValid(id, text string) bool {
	return len(id) >= 8 && len(id) <= 128 && strings.TrimSpace(text) != "" && len(text) <= 24000
}
func (s *Session) taskView(t *taskRecord) api.Task {
	v := t.View
	for _, p := range s.prompts {
		if p.thread == t.Thread {
			v.Status = "awaiting_approval"
			break
		}
	}
	if t.Pending != "" || (s.state.Connection != "ready" && !terminal(v.Status)) {
		v.Status = "unknown"
	}
	return v
}

// The host validates the selected directory. Native policy still gates commands;
// selecting a cwd does not grant ownership of another native conversation.
func (s *Session) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !taskRequestValid(in.RequestID, in.Prompt) || strings.TrimSpace(in.Title) == "" || len(in.Title) > 160 {
		return api.Task{}, errors.New("任务需要稳定请求标识、简短标题和明确要求")
	}
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	id, fingerprint := in.ID, opaque(in.Title, in.Prompt)
	if in.TaskStart.Workspace != "" {
		fingerprint = opaque(in.Title, in.Prompt, in.TaskStart.Workspace)
	}
	if !validWorkID(id) || !filepath.IsAbs(in.Workspace) || strings.TrimSpace(in.Instructions) == "" {
		return api.Task{}, errors.New("工作需要宿主分配的目录与角色")
	}
	s.mu.Lock()
	if t := s.binding.Tasks[id]; t != nil {
		v := t.View
		same := t.Fingerprint == fingerprint && t.View.Workspace == in.Workspace
		s.mu.Unlock()
		if !same {
			return v, errors.New("同一请求标识不能用于不同任务")
		}
		return v, nil
	}
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, err
	}
	if s.subscribedWorkerCount() >= workerSubscriptionLimit {
		s.mu.Unlock()
		return api.Task{}, errors.New("当前运行中的工作连接过多，请等待已完成任务释放资源后重试")
	}
	s.mu.Unlock()
	execution, err := s.resolveWorkExecution(ctx, in.Workspace)
	if err != nil {
		return api.Task{}, err
	}
	s.mu.Lock()
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, err
	}
	if s.subscribedWorkerCount() >= workerSubscriptionLimit {
		s.mu.Unlock()
		return api.Task{}, errors.New("当前运行中的工作连接过多，请等待已完成任务释放资源后重试")
	}
	workspace := in.Workspace
	t := &taskRecord{View: api.Task{ID: id, Title: in.Title, Workspace: workspace, Status: "unknown", Outcome: "unknown"}, Fingerprint: fingerprint, Source: s.binding.DelegationText, Requests: map[string]taskReceipt{}, Instructions: in.Instructions, Execution: &execution}
	if s.binding.Tasks == nil {
		s.binding.Tasks = map[string]*taskRecord{}
	}
	s.binding.Tasks[id] = t
	t.OriginalPrompt = in.Prompt
	if err := s.save(); err != nil {
		delete(s.binding.Tasks, id)
		s.mu.Unlock()
		return api.Task{}, err
	}
	c := s.client
	s.mu.Unlock()
	if err := validateWorkWorkspace(s.workRoot(), workspace, in.TaskStart.Workspace != ""); err != nil {
		return s.taskRejected(t, err)
	}
	if s.opts.BotTools != nil && withinBotWorkspace(s.opts.BotTools.NotebookDirectory, workspace) {
		return s.taskRejected(t, errors.New("Worker 工作目录不能使用 Bot 私有工作区"))
	}
	params := s.workerParams(workspace, in.Instructions, t)
	var response threadExecutionResponse
	err = callDecode(ctx, c, "thread/start", params, &response)
	s.mu.Lock()
	if err != nil || response.Thread.ID == "" || response.Model == "" || s.ownsThread(response.Thread.ID) {
		if err == nil {
			err = ErrProtocol
		}
		if definiteTaskRejection(err) {
			t.View.Status = "failed"
			t.View.Outcome = "rejected"
		}
		_ = s.save()
		v := t.View
		s.mu.Unlock()
		return v, err
	}
	t.Thread = response.Thread.ID
	t.Execution, t.ModelProvider = response.execution(), response.ModelProvider
	s.children[t.Thread] = true
	s.childWatching[t.Thread] = true // thread/start already subscribed this client.
	s.childSubscribed[t.Thread] = true
	delete(s.childRetired, t.Thread)
	// Ownership is durable before dispatch, so approvals cannot race adoption.
	if err = s.save(); err != nil {
		v := t.View
		s.mu.Unlock()
		return v, err
	}
	s.mu.Unlock()
	return s.sendTask(ctx, t, api.TaskMessage{ID: id, RequestID: in.RequestID, Prompt: in.Prompt}, false)
}

func withinBotWorkspace(bot, worker string) bool {
	if bot == "" {
		return false
	}
	rel, err := filepath.Rel(bot, worker)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func (s *Session) workRoot() string {
	if s.opts.WorkRoot != "" {
		return s.opts.WorkRoot
	}
	return filepath.Join(filepath.Dir(s.opts.Directory), "Tasks")
}
func validWorkID(id string) bool {
	if !strings.HasPrefix(id, "task-") || len(id) != 37 {
		return false
	}
	for _, c := range id[5:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validateWorkWorkspace(root, path string, selected bool) error {
	if selected {
		resolved, err := api.ResolveTaskWorkspace(path)
		if err != nil {
			return err
		}
		if resolved != path {
			return errors.New("工作目录已发生重定向")
		}
		return nil
	}
	if !filepath.IsAbs(root) || filepath.Dir(path) != root {
		return errors.New("工作目录不在宿主授权范围")
	}
	for _, p := range []string{root, path} {
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("工作目录必须是实际目录")
		}
	}
	return nil
}

// WorkAdmission validates native activation, not the application's task policy.
func (s *Session) WorkAdmission(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskAdmission()
}
func (s *Session) taskRejected(t *taskRecord, err error) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.View.Status = "failed"
	t.View.Outcome = "rejected"
	_ = s.save()
	return t.View, err
}
func definiteTaskRejection(err error) bool {
	var native *NativeError
	var request *RequestError
	return errors.As(err, &native) || (errors.As(err, &request) && !request.OutcomeUnknown)
}
func (s *Session) taskAdmission() error {
	if s.workerOnly && !s.closed && !s.closing && s.client != nil && s.client.Err() == nil && s.state.Connection == "ready" {
		return nil
	}
	if s.closed || s.closing || s.client == nil || s.state.Connection != "ready" || s.run == "" || s.binding.DelegationText == "" {
		return errors.New("只能在用户请求或已授权提醒激活的 Bot 回合中安排工作")
	}
	return nil
}
func (s *Session) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !taskRequestValid(in.RequestID, in.Prompt) {
		return api.Task{}, errors.New("需要稳定请求标识和任务要求")
	}
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t := s.binding.Tasks[in.ID]
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("只能继续 Bot 创建的任务")
	}
	if receipt, ok := t.Requests[in.RequestID]; ok {
		v := t.View
		v.Outcome = receipt.Outcome
		s.mu.Unlock()
		if receipt.Fingerprint != opaque(in.Prompt) {
			return v, errors.New("请求标识已用于其他内容")
		}
		return v, nil
	}
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, err
	}
	if t.Thread == "" || t.Pending != "" || t.View.Status == "unknown" || len(t.Requests) >= 100 {
		v := t.View
		s.mu.Unlock()
		return v, errors.New("请先核对该任务；结果未知的操作不会重复发送")
	}
	s.mu.Unlock()
	return s.sendTask(ctx, t, in, true)
}
func (s *Session) sendTask(ctx context.Context, t *taskRecord, in api.TaskMessage, resume bool) (api.Task, error) {
	s.mu.Lock()
	c := s.client
	run := s.childRuns[t.Thread]
	wasActive := t.View.Status == "working"
	if wasActive && run == "" {
		s.mu.Unlock()
		return api.Task{}, errors.New("运行回合尚未确认，请先读取任务")
	}
	previousView, previousRun, previousPending, previousSuppression := t.View, t.Run, t.Pending, t.SuppressReport
	previousSuperseded := append([]string(nil), t.SupersededRuns...)
	t.Requests[in.RequestID] = taskReceipt{Fingerprint: opaque(in.Prompt), Outcome: "unknown", Phase: "prepared", PriorStatus: t.View.Status, PriorRun: t.Run, PriorResult: t.View.Result, PriorSuppressed: t.SuppressReport}
	t.Pending = in.RequestID
	t.SuppressReport = false
	if !wasActive {
		if t.Run != "" && !slices.Contains(t.SupersededRuns, t.Run) {
			t.SupersededRuns = append(t.SupersededRuns, t.Run)
		}
		t.Run = ""
	}
	t.View.Status = "unknown"
	t.View.Outcome = "unknown"
	t.View.Result = ""
	if err := s.save(); err != nil {
		delete(t.Requests, in.RequestID)
		t.View, t.Run, t.Pending, t.SuppressReport, t.SupersededRuns = previousView, previousRun, previousPending, previousSuppression, previousSuperseded
		s.mu.Unlock()
		return api.Task{}, err
	}
	params := map[string]any{"threadId": t.Thread, "clientUserMessageId": in.RequestID}
	// Provenance stays in the binding. The worker receives the actual assignment.
	params["input"] = []nativeInput{{Type: "text", Text: in.Prompt, TextElements: []any{}}}
	s.mu.Unlock()
	// Reattach an owned, idle thread after reconnect. This never imports App tasks.
	if resume && !wasActive {
		p := s.workerParams(t.View.Workspace, t.Instructions, t)
		p["threadId"] = t.Thread
		p["excludeTurns"] = true
		var response threadExecutionResponse
		if err := callDecode(ctx, c, "thread/resume", p, &response); err != nil || response.Thread.ID != t.Thread || response.Model == "" {
			if err == nil {
				err = ErrProtocol
			}
			return s.taskSendResult(t, in.RequestID, nativeTurn{}, &RequestError{Method: "turn/start", OutcomeUnknown: false, Cause: err})
		}
		s.mu.Lock()
		s.childWatching[t.Thread] = true
		s.childSubscribed[t.Thread] = true
		delete(s.childRetired, t.Thread)
		s.childRetireSeq[t.Thread]++
		t.Execution, t.ModelProvider = response.execution(), response.ModelProvider
		err := s.save()
		s.mu.Unlock()
		if err != nil {
			return s.taskSendResult(t, in.RequestID, nativeTurn{}, &RequestError{Method: "turn/start", OutcomeUnknown: false, Cause: err})
		}
	}
	s.mu.Lock()
	record := t.Requests[in.RequestID]
	record.Phase = "dispatching"
	t.Requests[in.RequestID] = record
	phaseErr := s.save()
	s.mu.Unlock()
	if phaseErr != nil {
		return s.taskSendResult(t, in.RequestID, nativeTurn{}, &RequestError{Method: "turn/start", Cause: phaseErr})
	}
	method := "turn/start"
	if wasActive {
		method = "turn/steer"
		params["expectedTurnId"] = run
	} else {
		s.applyWorkExecution(params, false, t)
		if policy, ok := params["sandboxPolicy"].(map[string]any); ok && policy["type"] == "workspaceWrite" {
			policy["writableRoots"] = []string{t.View.Workspace}
		}
	}
	var response struct {
		Turn   nativeTurn `json:"turn"`
		TurnID string     `json:"turnId"`
	}
	err := callDecode(ctx, c, method, params, &response)
	if wasActive {
		if err == nil && response.TurnID != run {
			err = ErrProtocol
		}
		response.Turn = nativeTurn{ID: run, Status: "inProgress"}
	}
	if err == nil && response.Turn.ID == "" {
		err = ErrProtocol
	}
	return s.taskSendResult(t, in.RequestID, response.Turn, err)
}
func (s *Session) taskSendResult(t *taskRecord, request string, turn nativeTurn, err error) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := t.Requests[request]
	if err == nil {
		r.Outcome = "accepted"
		r.Phase = "settled"
		t.Pending = ""
		// The native acceptance has settled this exact request before the
		// returned turn is projected; keep the unknown guard for other requests.
		t.Requests[request] = r
		// Notifications may have already completed this turn or started a later
		// human turn. A delayed RPC receipt must not rewind that live state.
		if t.Run == "" || t.Run == turn.ID || slices.Contains(t.SupersededRuns, t.Run) {
			s.observeTaskTurn(t, turn)
			if status := s.childTerminalStatus[opaque(t.Thread, turn.ID)]; status != "" && t.Run == turn.ID && !terminal(t.View.Status) {
				s.observeTaskTurn(t, nativeTurn{ID: turn.ID, Status: status})
			}
			if !s.childTerminals[opaque(t.Thread, turn.ID)] && !terminal(t.View.Status) {
				s.childRuns[t.Thread] = turn.ID
			}
		}
	} else if definiteTaskRejection(err) {
		r.Outcome = "rejected"
		r.Phase = "settled"
		t.Pending = ""
		if t.Run == "" || t.Run == r.PriorRun {
			t.View.Status, t.View.Result, t.Run, t.SuppressReport = r.PriorStatus, r.PriorResult, r.PriorRun, r.PriorSuppressed
			if r.PriorRun != "" && terminal(r.PriorStatus) {
				t.SupersededRuns = slices.DeleteFunc(t.SupersededRuns, func(run string) bool { return run == r.PriorRun })
			}
			if t.View.Status == "unknown" && t.Run == "" {
				t.View.Status = "failed"
			}
		}
	}
	if old := t.Requests[request]; old.Outcome == "accepted" {
		r.Outcome = "accepted"
	}
	t.Requests[request] = r
	t.View.Outcome = r.Outcome
	// Reconcile an uncertain receipt once; normal progress is subscription-driven.
	if t.Thread != "" && err != nil && !s.childWatching[t.Thread] {
		s.childWatching[t.Thread] = true
		go s.watchChild(s.client, s.epoch, t.Thread)
	}
	if saveErr := s.save(); saveErr != nil {
		err = saveErr
	}
	s.update()
	return t.View, err
}
func (s *Session) observeTaskTurn(t *taskRecord, turn nativeTurn) (changed bool) {
	view, pending, run, report, reportState := t.View, t.Pending, t.Run, t.ReportID, t.ReportState
	defer func() {
		changed = changed || view != t.View || pending != t.Pending || run != t.Run || report != t.ReportID || reportState != t.ReportState
	}()
	if turn.ID == "" {
		return
	}
	for _, item := range turn.Items {
		if item.Type == "userMessage" {
			if r, ok := t.Requests[item.ClientID]; ok {
				changed = changed || r.Outcome != "accepted"
				r.Outcome = "accepted"
				r.Phase = "settled"
				t.Requests[item.ClientID] = r
				if item.ClientID == t.Pending {
					t.Pending = ""
					t.View.Outcome = "accepted"
				}
			}
		}
	}
	if terminal(turn.Status) {
		s.childTerminalStatus[opaque(t.Thread, turn.ID)] = turn.Status
	}
	if slices.Contains(t.SupersededRuns, turn.ID) {
		return
	}
	// A terminal fact for an earlier run cannot settle a later submission whose
	// original client identity has not appeared in this native thread snapshot.
	if t.Pending != "" || taskHasUnknownReceipt(t) {
		if t.Run == "" {
			t.Run = turn.ID
		}
		t.View.Status = "unknown"
		t.View.Outcome = "unknown"
		return
	}
	if t.Run != turn.ID {
		if s.childTerminals[opaque(t.Thread, turn.ID)] {
			return
		}
		t.Run = turn.ID
		t.SuppressReport = false
		t.View.Result = ""
	}
	if turn.Status == "inProgress" && !s.childTerminals[opaque(t.Thread, turn.ID)] {
		t.View.Status = "working"
	}
	if terminal(turn.Status) {
		t.Run = turn.ID
		t.View.Status = turn.Status
		reportID := "task-report-" + opaque(t.Thread, turn.ID)
		if t.ReportID != reportID {
			t.ReportID = reportID
			t.ReportState = "pending"
		}
		if t.SuppressReport {
			t.ReportState = "observed"
		}
		for _, item := range turn.Items {
			if item.Type == "agentMessage" {
				t.View.Result = boundedText(item.Text, 6000)
			}
		}
	}
	return
}

// A prepared receipt was durably recorded before any native send was admitted.
// Crashes here can settle as unsent. Legacy/dispatching receipts remain unknown.
func settlePreparedTask(t *taskRecord) {
	if t == nil || t.Pending == "" {
		return
	}
	r, ok := t.Requests[t.Pending]
	if !ok || r.Phase != "prepared" || r.Outcome != "unknown" {
		return
	}
	r.Outcome, r.Phase = "rejected", "settled"
	t.Requests[t.Pending] = r
	t.Pending = ""
	t.Run, t.View.Status, t.View.Result, t.SuppressReport = r.PriorRun, r.PriorStatus, r.PriorResult, r.PriorSuppressed
	t.View.Outcome = "rejected"
	t.SupersededRuns = slices.DeleteFunc(t.SupersededRuns, func(id string) bool { return id == r.PriorRun })
}
func (s *Session) supersededTaskRun(thread, run string) bool {
	t := s.taskByThread(thread)
	return t != nil && slices.Contains(t.SupersededRuns, run)
}
func boundedText(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return text
}

func (s *Session) ReadWork(ctx context.Context, id string) (api.Task, error) {
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t := s.binding.Tasks[id]
	c := s.client
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("只能读取 Bot 创建的任务")
	}
	if t.Thread == "" || c == nil || s.state.Connection != "ready" {
		v := t.View
		s.mu.Unlock()
		return v, errors.New("任务创建或连接结果尚未确认，不会自动重建")
	}
	revision := s.childRevision[t.Thread]
	s.mu.Unlock()
	thread, err := readThreadState(ctx, c, t.Thread)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		return t.View, err
	}
	if thread.ID != t.Thread {
		return t.View, ErrProtocol
	}
	if revision == s.childRevision[t.Thread] {
		for _, turn := range thread.Turns {
			s.observeTaskTurn(t, turn)
		}
		// An idle native thread with no terminal fact for the current run
		// cannot confirm the continuation's result, even if older turns are
		// complete. A newer live event supersedes this read entirely.
		if (thread.Status.Type == "idle" || thread.Status.Type == "notLoaded") && t.View.Status == "working" {
			t.View.Status = "unknown"
		}
	}
	if thread.Status.Type == "active" {
		if !s.childWatching[t.Thread] {
			s.childWatching[t.Thread] = true
			go s.watchChild(c, s.epoch, t.Thread)
		}
	} else if (thread.Status.Type == "idle" || thread.Status.Type == "notLoaded") && terminal(t.View.Status) {
		delete(s.childRuns, t.Thread)
		s.scheduleChildRetirement(t.Thread)
	}
	if err = s.save(); err != nil {
		return t.View, err
	}
	return s.taskView(t), nil
}
func (s *Session) StopWork(ctx context.Context, id string) (api.Task, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t := s.binding.Tasks[id]
	c := s.client
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("只能停止 Bot 创建的任务")
	}
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, err
	}
	run := s.childRuns[t.Thread]
	v := t.View
	if run == "" {
		s.mu.Unlock()
		return v, errors.New("没有已确认的运行回合，请先读取任务")
	}
	s.mu.Unlock()
	err := callDecode(ctx, c, "turn/interrupt", map[string]any{"threadId": t.Thread, "turnId": run}, &struct{}{})
	s.mu.Lock()
	defer s.mu.Unlock()
	return t.View, err
}

// Notices retain their origin even though the native wire accepts text input.
func (s *Session) SubmitReport(ctx context.Context, in api.Submission) (api.Receipt, error) {
	return s.submitWithSource(ctx, in, nil, true, true)
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.ReportSubmitter = (*Session)(nil)
