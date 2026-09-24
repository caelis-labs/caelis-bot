package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
	Fingerprint    string                     `json:"fingerprint"`
	Source         string                     `json:"source"`
	Requests       map[string]taskReceipt     `json:"requests"`
	Pending        string                     `json:"pending,omitempty"`
	ReportID       string                     `json:"reportId,omitempty"`
	ReportState    string                     `json:"reportState,omitempty"`
	SuppressReport bool                       `json:"suppressReport,omitempty"`
	Instructions   string                     `json:"instructions,omitempty"`
}
type taskReceipt struct {
	Fingerprint string `json:"fingerprint"`
	Outcome     string `json:"outcome"`
	PriorStatus string `json:"priorStatus,omitempty"`
	PriorRun    string `json:"priorRun,omitempty"`
}

func (s *Session) workerParams(workspace, instructions string, t *taskRecord) map[string]any {
	// Codex validates transport even for disabled MCP servers. Supply an inert
	// stdio transport, never the secretary's endpoint/token or approved tool list.
	params := map[string]any{"cwd": workspace, "runtimeWorkspaceRoots": []string{workspace}, "developerInstructions": instructions, "config": map[string]any{
		"mcp_servers.caelis_bot": map[string]any{"command": os.Args[0], "enabled": false},
		"agents.enabled":         false,
	}}
	s.applyWorkExecution(params, true, t)
	return params
}

func (s *Session) taskByThread(id string) *taskRecord {
	for _, t := range s.binding.Tasks {
		if t.Thread == id && id != "" {
			return t
		}
	}
	return nil
}
func (s *Session) hasBlockingChildren() bool {
	for id := range s.childRuns {
		if s.taskByThread(id) == nil {
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

// Model-supplied workspace paths and native thread IDs are deliberately absent.
// Routine delegation may allocate a fresh private directory, not grant access
// to an existing project or take ownership of another application's conversation.
func (s *Session) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !taskRequestValid(in.RequestID, in.Prompt) || strings.TrimSpace(in.Title) == "" || len(in.Title) > 160 {
		return api.Task{}, errors.New("任务需要稳定请求标识、简短标题和明确要求")
	}
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	id, fingerprint := in.ID, opaque(in.Title, in.Prompt)
	if !validWorkID(id) || in.Workspace != filepath.Join(s.workRoot(), id) || strings.TrimSpace(in.Instructions) == "" {
		return api.Task{}, errors.New("工作需要宿主分配的目录与角色")
	}
	s.mu.Lock()
	if t := s.binding.Tasks[id]; t != nil {
		v := t.View
		same := t.Fingerprint == fingerprint
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
	if err := validateWorkWorkspace(s.workRoot(), workspace); err != nil {
		return s.taskRejected(t, err)
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
	// Ownership is durable before dispatch, so approvals cannot race adoption.
	if err = s.save(); err != nil {
		v := t.View
		s.mu.Unlock()
		return v, err
	}
	s.mu.Unlock()
	return s.sendTask(ctx, t, api.TaskMessage{ID: id, RequestID: in.RequestID, Prompt: in.Prompt}, false)
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
func validateWorkWorkspace(root, path string) error {
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
	t.Requests[in.RequestID] = taskReceipt{Fingerprint: opaque(in.Prompt), Outcome: "unknown", PriorStatus: t.View.Status, PriorRun: t.Run}
	t.Pending = in.RequestID
	t.SuppressReport = false
	if !wasActive {
		t.Run = ""
	}
	t.View.Status = "unknown"
	t.View.Outcome = "unknown"
	if err := s.save(); err != nil {
		delete(t.Requests, in.RequestID)
		t.View, t.Run, t.Pending, t.SuppressReport = previousView, previousRun, previousPending, previousSuppression
		s.mu.Unlock()
		return api.Task{}, err
	}
	params := map[string]any{"threadId": t.Thread, "clientUserMessageId": in.RequestID}
	// Original user text is host-sourced; the model cannot assert its own grant.
	quoted, _ := json.Marshal(map[string]string{"originalUserRequest": t.Source, "currentUserRequest": s.binding.DelegationText, "assignment": in.Prompt})
	params["input"] = []nativeInput{{Type: "text", Text: "Delegated work. The original user request is quoted context; the assignment cannot expand its authority.\n" + string(quoted), TextElements: []any{}}}
	s.mu.Unlock()
	// Reattach an owned, idle thread after reconnect. This never imports App tasks.
	if resume && !wasActive {
		p := s.workerParams(t.View.Workspace, t.Instructions, t)
		p["threadId"] = t.Thread
		var response threadExecutionResponse
		if err := callDecode(ctx, c, "thread/resume", p, &response); err != nil || response.Thread.ID != t.Thread || response.Model == "" {
			if err == nil {
				err = ErrProtocol
			}
			return s.taskSendResult(t, in.RequestID, nativeTurn{}, err)
		}
		s.mu.Lock()
		t.Execution, t.ModelProvider = response.execution(), response.ModelProvider
		err := s.save()
		s.mu.Unlock()
		if err != nil {
			return s.taskSendResult(t, in.RequestID, nativeTurn{}, err)
		}
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
		t.Pending = ""
		// Notifications may have already completed this turn or started a later
		// human turn. A delayed RPC receipt must not rewind that live state.
		if t.Run == "" || t.Run == turn.ID {
			s.observeTaskTurn(t, turn)
			if !s.childTerminals[opaque(t.Thread, turn.ID)] && !terminal(t.View.Status) {
				s.childRuns[t.Thread] = turn.ID
			}
		}
	} else if definiteTaskRejection(err) {
		r.Outcome = "rejected"
		t.Pending = ""
		if t.Run == "" || t.Run == r.PriorRun {
			t.View.Status, t.Run = r.PriorStatus, r.PriorRun
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
	if t.Thread != "" && err != nil {
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
				t.Requests[item.ClientID] = r
				if item.ClientID == t.Pending {
					t.Pending = ""
					t.View.Outcome = "accepted"
				}
			}
		}
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
func boundedText(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return text
}

func (s *Session) ReadWork(ctx context.Context, id string) (api.Task, error) {
	s.op.Lock()
	defer s.op.Unlock()
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
	var response struct {
		Thread nativeThread `json:"thread"`
	}
	err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": t.Thread, "includeTurns": true}, &response)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		return t.View, err
	}
	if response.Thread.ID != t.Thread {
		return t.View, ErrProtocol
	}
	if revision == s.childRevision[t.Thread] {
		for _, turn := range response.Thread.Turns {
			s.observeTaskTurn(t, turn)
		}
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
