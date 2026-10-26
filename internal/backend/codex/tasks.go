package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

// Task records and submission receipts share the atomic conversation binding.
// A recorded unknown outcome is never permission to retry a mutation.
type taskRecord struct {
	LastError         *api.TaskFailure               `json:"lastError,omitempty"`
	AsyncAdmission    asyncAdmission                 `json:"asyncAdmission,omitempty"`
	Owner             string                         `json:"owner,omitempty"`
	Build             string                         `json:"build,omitempty"`
	StartRequest      string                         `json:"startRequest,omitempty"`
	StartIntent       string                         `json:"startIntent,omitempty"`
	OriginalPrompt    string                         `json:"originalPrompt,omitempty"`
	Execution         *api.WorkExecutionSettings     `json:"execution,omitempty"`
	ModelProvider     string                         `json:"modelProvider,omitempty"`
	View              api.Task                       `json:"view"`
	Thread            string                         `json:"thread"`
	Run               string                         `json:"run"`
	Artifacts         map[string]string              `json:"workArtifacts,omitempty"`
	ApprovalEvidence  []workerApprovalEvidence       `json:"approvalEvidence,omitempty"`
	AsyncQuestions    map[string]workerAsyncQuestion `json:"asyncQuestions,omitempty"`
	CompletedMessages map[string]bool                `json:"completedMessages,omitempty"` // exact native turn/item completion receipts
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
	Activity       string                 `json:"-"`

	StopPendingRequest string `json:"stopPendingRequest,omitempty"` // original request to stop after an uncertain retry dispatch
	StopPendingTurn    string `json:"stopPendingTurn,omitempty"`
	StopPendingPhase   string `json:"stopPendingPhase,omitempty"`  // requested, attempted, acknowledged
	StopPendingStatus  string `json:"stopPendingStatus,omitempty"` // authoritative original native terminal status
}
type workerAsyncQuestion struct {
	Admitted   bool                `json:"admitted,omitempty"`
	ObservedAt int64               `json:"observedAt,omitempty"`
	Digest     string              `json:"digest,omitempty"` // Payload tombstone; native transcript stays authoritative.
	Run        string              `json:"run"`
	CallID     string              `json:"callId"`
	Questions  []api.AsyncQuestion `json:"questions"`
}

// Worker question payloads are a temporary observation cache, separate from
// original input receipts and the native transcript. Keep only a digest after
// its fixed lifetime; reconnect/replay cannot give the same call a new deadline.
func compactWorkerQuestions(t *taskRecord) {
	if t == nil {
		return
	}
	for key, q := range t.AsyncQuestions {
		if q.ObservedAt == 0 {
			q.ObservedAt = time.Now().Unix()
		}
		if q.Digest == "" {
			payload, _ := json.Marshal(q.Questions)
			q.Digest = opaque(string(payload))
		}
		if time.Since(time.Unix(q.ObservedAt, 0)) >= 24*time.Hour {
			delete(t.AsyncQuestions, key)
			continue
		}
		t.AsyncQuestions[key] = q
	}
}

func (s *Session) recordWorkerAsync(t *taskRecord, run string, item nativeItem) bool {
	if t == nil || run == "" || item.ID == "" || item.Type != "agentMessage" || item.Delivery != "async" || len(item.Questions) == 0 {
		return false
	}
	questions := make([]api.AsyncQuestion, 0, len(item.Questions))
	for _, q := range item.Questions {
		if strings.TrimSpace(q.Title) == "" {
			return false
		}
		questions = append(questions, api.AsyncQuestion{Title: q.Title, Options: append([]string(nil), q.Options...)})
	}
	if t.AsyncAdmission.expiry(run) <= time.Now().Unix() {
		// Legacy registered payloads remain readable with their original deadline.
		return false
	}
	if t.AsyncQuestions == nil {
		t.AsyncQuestions = map[string]workerAsyncQuestion{}
	}
	compactWorkerQuestions(t)
	key := opaque(run, item.ID)
	if _, exists := t.AsyncQuestions[key]; !exists && len(t.AsyncQuestions) >= 4096 {
		return false
	}
	payload, _ := json.Marshal(questions)
	fingerprint := opaque(string(payload))
	if old, ok := t.AsyncQuestions[key]; ok && old.Run == run && old.CallID == item.ID {
		if old.Digest == fingerprint && old.ObservedAt > 0 && time.Since(time.Unix(old.ObservedAt, 0)) >= 24*time.Hour {
			return false
		}
		if slices.EqualFunc(old.Questions, questions, func(a, b api.AsyncQuestion) bool { return a.Title == b.Title && slices.Equal(a.Options, b.Options) }) {
			return false
		}
	}
	t.AsyncQuestions[key] = workerAsyncQuestion{Admitted: true, Run: run, CallID: item.ID, Questions: questions, ObservedAt: min(time.Now().Unix(), t.AsyncAdmission.Deadline-int64(24*time.Hour/time.Second)), Digest: fingerprint}
	return true
}

type taskReceipt struct {
	Kind              string           `json:"kind,omitempty"`
	IntentFingerprint string           `json:"intentFingerprint,omitempty"`
	ExecutionKey      string           `json:"executionKey,omitempty"`
	ControlTarget     string           `json:"controlTarget,omitempty"`
	Fingerprint       string           `json:"fingerprint"`
	Outcome           string           `json:"outcome"`
	PriorStatus       string           `json:"priorStatus,omitempty"`
	PriorRun          string           `json:"priorRun,omitempty"`
	PriorFailure      *api.TaskFailure `json:"priorFailure,omitempty"`
	PriorResult       string           `json:"priorResult,omitempty"`
	PriorSuppressed   bool             `json:"priorSuppressed,omitempty"`
	Phase             string           `json:"phase,omitempty"`
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
	for taskID, t := range s.binding.Tasks {
		if t != nil && !s.binding.LostOwnerTasks[taskID] && t.Thread == id && id != "" {
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
	for id, task := range s.binding.Tasks {
		if task != nil && !s.binding.LostOwnerTasks[id] && (task.Pending != "" || taskHasUnknownReceipt(task) || !terminal(task.View.Status)) {
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
	return s.workStatesLocked()
}
func (s *Session) workStatesLocked() []api.WorkState {
	out := []api.WorkState{}
	for id, t := range s.binding.Tasks {
		v := s.taskView(t)
		activity := t.Activity
		if s.binding.LostOwnerTasks[id] || s.binding.OwnerUnavailable && s.ownerUnavailableVerified {
			activity = api.WorkActivityOwnerUnavailable
		} else if s.state.Connection != "ready" {
			activity = ""
		}
		originalStopCurrent := taskStopCurrent(t)
		resolution := ""
		if originalStopCurrent {
			if !s.stopTerminalSavePending["task:"+t.View.ID] {
				resolution = t.StopPendingStatus
			}
		}
		state := api.WorkState{TurnKey: opaque(t.Thread, t.Run), Task: v, Activity: activity, OriginalPrompt: t.OriginalPrompt, StopExecutionKey: s.workExecutionKeyLocked(t), ExecutionKey: t.ReportID, StopRequested: t.SuppressReport || originalStopCurrent, StopAcknowledged: s.workerStopAcknowledged(t), StopPending: originalStopCurrent && resolution == "", StopResolution: resolution, PreviousReportID: t.ReportID, PreviousReportState: t.ReportState, StartFingerprint: t.Fingerprint}
		for key, question := range t.AsyncQuestions {
			if len(question.Questions) == 0 || question.ObservedAt > 0 && time.Since(time.Unix(question.ObservedAt, 0)) >= 24*time.Hour {
				continue
			}
			// A reply starts a new native turn, but unanswered fields in the
			// original async item remain addressable by their original call ID.
			expiry := int64(0)
			if !question.Admitted {
				expiry = -1
			} else if question.ObservedAt > 0 {
				expiry = question.ObservedAt + int64(24*time.Hour/time.Second)
			}
			state.AsyncQuestions = append(state.AsyncQuestions, api.Item{ID: "worker-question:" + opaque(t.Thread, key), TurnKey: opaque(t.Thread, question.Run), Kind: "assistant", Status: "completed", AsyncCallID: question.CallID, AsyncExpiresAt: expiry, AsyncQuestions: append([]api.AsyncQuestion(nil), question.Questions...)})
		}
		sort.Slice(state.AsyncQuestions, func(i, j int) bool { return state.AsyncQuestions[i].ID < state.AsyncQuestions[j].ID })
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Task.ID < out[j].Task.ID })
	return out
}
func taskStopCurrent(t *taskRecord) bool {
	return t.StopPendingRequest != "" && (t.StopPendingStatus == "" || t.StopPendingTurn == t.Run)
}
func taskRequestValid(id, text string) bool {
	return len(id) >= 8 && len(id) <= 128 && strings.TrimSpace(text) != "" && len(text) <= 24000
}
func (s *Session) taskView(t *taskRecord) api.Task {
	v := t.View
	if s.binding.LostOwnerTasks[v.ID] {
		if !terminal(v.Status) || t.Pending != "" || taskHasUnknownReceipt(t) || t.StopPendingRequest != "" && t.StopPendingStatus == "" {
			v.Status = "unknown"
			v.Result = "原任务运行环境尚未确认；请求与工具效果仍待核对，原回执已保留"
		}
		return v
	}
	for _, p := range s.prompts {
		if p.thread == t.Thread {
			v.Status = "awaiting_approval"
			break
		}
	}
	waitingForCapacity := s.capacityWaiting && s.capacityThread == t.Thread && v.Status == "pending"
	if !waitingForCapacity && (t.Pending != "" || taskHasUnknownReceipt(t) || (s.state.Connection != "ready" && !terminal(v.Status))) {
		v.Status = "unknown"
	}
	if t.StopPendingRequest != "" && (t.StopPendingStatus == "" || t.StopPendingTurn == t.Run) {
		if s.stopTerminalSavePending["task:"+t.View.ID] {
			v.Status = "unknown"
			v.Result = "原工作终态已观察到，但本地记录尚未保存；正在重试，暂不确认停止结果"
		} else if t.StopPendingStatus == "" {
			v.Status = "unknown"
			v.Result = "已请求停止原工作，原生结果尚未确认；勿重复派发工作"
		} else if t.StopPendingStatus == "interrupted" {
			v.Result = "原回合已中断；已执行工具的实际效果仍以原生回执为准。" + v.Result
		} else {
			v.Result = "停止请求未确认及时生效；原回合实际状态为" + t.StopPendingStatus + "。" + v.Result
		}
	}
	return v
}

func nativeTaskActive(thread nativeThread) bool {
	if thread.Status.Type == "active" {
		return true
	}
	for _, turn := range thread.Turns {
		if turn.Status == "inProgress" {
			return true
		}
	}
	return false
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
	digests, err := api.WorkInputDigests(in.Files)
	if err != nil {
		return api.Task{}, err
	}
	if in.FileDigests != nil && !slices.Equal(in.FileDigests, digests) {
		return api.Task{}, errors.New("Worker attachment intent changed")
	}
	in.FileDigests = digests
	id, fingerprint := in.ID, opaque(in.Title, in.Prompt)
	if in.TaskStart.Workspace != "" {
		fingerprint = opaque(in.Title, in.Prompt, in.TaskStart.Workspace)
	}
	if len(digests) > 0 {
		fingerprint = opaque(fingerprint, api.WorkFingerprint(digests))
	}
	if !validWorkID(id) || !filepath.IsAbs(in.Workspace) || strings.TrimSpace(in.Instructions) == "" {
		return api.Task{}, errors.New("工作需要宿主分配的目录与角色")
	}
	s.mu.Lock()
	if t := s.binding.Tasks[id]; t != nil {
		v := t.View
		same := t.Fingerprint == fingerprint && t.View.Workspace == in.Workspace && (t.StartIntent == "" || t.StartIntent == api.WorkFingerprint(in))
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
	var execution api.WorkExecutionSettings
	if in.ExecutionOverride != nil {
		execution = *in.ExecutionOverride
	} else {
		execution, err = s.resolveWorkExecution(ctx, in.Workspace)
	}
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
	t := &taskRecord{View: api.Task{ID: id, Title: in.Title, Workspace: workspace, Status: "unknown", Outcome: "unknown"}, Owner: s.workOwnerLocked(), Build: s.workBuildLocked(), StartRequest: in.RequestID, StartIntent: api.WorkFingerprint(in), Fingerprint: fingerprint, Source: s.binding.DelegationText, Requests: map[string]taskReceipt{}, Instructions: in.Instructions, Execution: &execution}
	if s.binding.Tasks == nil {
		s.binding.Tasks = map[string]*taskRecord{}
	}
	s.binding.Tasks[id] = t
	t.OriginalPrompt = in.Prompt
	t.Requests[in.RequestID] = taskReceipt{Kind: "start", IntentFingerprint: t.StartIntent, Outcome: "unknown", Phase: "creating"}
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
			r := t.Requests[in.RequestID]
			r.Outcome = "rejected"
			r.Phase = "settled"
			t.Requests[in.RequestID] = r
		}
		_ = s.save()
		v := t.View
		s.mu.Unlock()
		if definiteTaskRejection(err) {
			err = &api.WorkMessageRejection{Cause: err}
		}
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
	return s.sendTask(ctx, t, api.TaskMessage{ID: id, RequestID: in.RequestID, Prompt: in.Prompt, Files: in.Files, FileDigests: in.FileDigests}, false)
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
	if r, ok := t.Requests[t.StartRequest]; ok {
		r.Outcome = "rejected"
		r.Phase = "settled"
		t.Requests[t.StartRequest] = r
	}
	_ = s.save()
	return t.View, &api.WorkMessageRejection{Cause: err}
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
	digests, err := api.WorkInputDigests(in.Files)
	if err != nil {
		return api.Task{}, &api.WorkMessageRejection{Cause: err}
	}
	if in.FileDigests != nil && !slices.Equal(in.FileDigests, digests) {
		return api.Task{}, &api.WorkMessageRejection{Cause: errors.New("Worker attachment intent changed")}
	}
	in.FileDigests = digests
	if !taskRequestValid(in.RequestID, in.Prompt) {
		return api.Task{}, &api.WorkMessageRejection{Cause: errors.New("需要稳定请求标识和任务要求")}
	}
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t := s.binding.Tasks[in.ID]
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, &api.WorkMessageRejection{Cause: errors.New("只能继续 Bot 创建的任务")}
	}
	if receipt, ok := t.Requests[in.RequestID]; ok {
		v := s.taskView(t)
		v.Outcome = receipt.Outcome
		s.mu.Unlock()
		if receipt.Fingerprint != workMessageFingerprint(in) || receipt.IntentFingerprint != "" && receipt.Kind == "send" && receipt.IntentFingerprint != api.WorkFingerprint(in) {
			return v, errors.New("请求标识已用于其他内容")
		}
		return v, nil
	}
	if s.binding.LostOwnerTasks[in.ID] {
		v := s.taskView(t)
		s.mu.Unlock()
		return v, &api.WorkMessageRejection{Cause: errors.New("原 owner 已关闭，不能向新连接重发或续接旧任务")}
	}
	// Answering a validated original question is an interaction, independent
	// of resident activation. Ordinary continuation keeps native admission.
	if callID := api.WorkerQuestionAnswer(ctx); callID != "" {
		found := false
		for _, q := range t.AsyncQuestions {
			if q.CallID == callID && len(q.Questions) > 0 {
				found = true
			}
		}
		if !found {
			s.mu.Unlock()
			return api.Task{}, &api.WorkMessageRejection{Code: "original_question_unavailable", Cause: errors.New("original Worker question unavailable")}
		}
		if s.closed || s.closing || s.client == nil || s.client.Err() != nil || s.state.Connection != "ready" {
			s.mu.Unlock()
			return api.Task{}, &api.WorkMessageRejection{Code: "owner_unavailable", Cause: api.ErrRecoveryPending}
		}
	} else if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, &api.WorkMessageRejection{Code: "activation_required", Cause: err}
	}
	if t.Thread == "" || t.Pending != "" || s.taskView(t).Status == "unknown" || s.taskView(t).Status == "pending" {
		v := t.View
		s.mu.Unlock()
		return v, &api.WorkMessageRejection{Code: "original_receipt_unresolved", Cause: errors.New("请先核对该任务；结果未知的操作不会重复发送")}
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
		return api.Task{}, &api.WorkMessageRejection{Cause: errors.New("运行回合尚未确认，请先读取任务")}
	}
	previousView, previousRun, previousPending, previousSuppression := t.View, t.Run, t.Pending, t.SuppressReport
	previousSuperseded := append([]string(nil), t.SupersededRuns...)
	kind, intent := "send", api.WorkFingerprint(in)
	if in.RequestID == t.StartRequest {
		kind, intent = "start", t.StartIntent
	}
	t.Requests[in.RequestID] = taskReceipt{Kind: kind, IntentFingerprint: intent, Fingerprint: workMessageFingerprint(in), Outcome: "unknown", Phase: "prepared", PriorStatus: t.View.Status, PriorRun: t.Run, PriorResult: t.View.Result, PriorFailure: t.View.Failure, PriorSuppressed: t.SuppressReport}
	t.Pending = in.RequestID
	t.Activity = "" // New dispatch invalidates preceding idle capacity evidence.
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
	t.View.Failure = nil
	if err := s.save(); err != nil {
		delete(t.Requests, in.RequestID)
		t.View, t.Run, t.Pending, t.SuppressReport, t.SupersededRuns = previousView, previousRun, previousPending, previousSuppression, previousSuperseded
		s.mu.Unlock()
		return api.Task{}, &api.WorkMessageRejection{Cause: err}
	}
	params := map[string]any{"threadId": t.Thread, "clientUserMessageId": in.RequestID}
	// Provenance stays in the binding. The worker receives the actual assignment.
	text := in.Prompt
	s.mu.Unlock()
	input, inputErr := s.prepareInput(api.Submission{Text: text}, in.Files, nil)
	if inputErr != nil {
		return s.taskSendResult(t, in.RequestID, nativeTurn{}, &RequestError{Method: "turn/start", OutcomeUnknown: false, Cause: inputErr})
	}
	if after, e := api.WorkInputDigests(in.Files); e != nil || !slices.Equal(after, in.FileDigests) {
		if e == nil {
			e = errors.New("Worker attachment changed while staging")
		}
		return s.taskSendResult(t, in.RequestID, nativeTurn{}, &RequestError{Method: "turn/start", OutcomeUnknown: false, Cause: e})
	}
	params["input"] = input
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
	err := s.startWithCapacityRetry(ctx, c, method, params, &response, t.Thread, in.RequestID)
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
		if t.StopPendingRequest == request {
			t.StopPendingTurn = turn.ID
		}
		t.Pending = ""
		// The native acceptance has settled this exact request before the
		// returned turn is projected; keep the unknown guard for other requests.
		t.Requests[request] = r
		// Notifications may have already completed this turn or started a later
		// human turn. A delayed RPC receipt must not rewind that live state.
		if t.Run == "" || t.Run == turn.ID || slices.Contains(t.SupersededRuns, t.Run) {
			t.AsyncAdmission.begin(turn.ID)
			s.observeTaskTurn(t, turn)
			if ended := s.childTerminalStatus[opaque(t.Thread, turn.ID)]; ended.Status != "" && t.Run == turn.ID && !terminal(t.View.Status) {
				s.observeTaskTurn(t, nativeTurn{ID: turn.ID, Status: ended.Status})
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
			t.View.Failure = r.PriorFailure
			if r.PriorRun != "" && terminal(r.PriorStatus) {
				t.SupersededRuns = slices.DeleteFunc(t.SupersededRuns, func(run string) bool { return run == r.PriorRun })
			}
			if t.View.Status == "unknown" && t.Run == "" {
				t.View.Status = "failed"
			}
			if capacityReportedError(err) && t.View.Status == "failed" {
				t.View.Result = "模型仍然繁忙，自动重试已耗尽；请稍后继续原任务或手动选择其他模型"
			} else if errors.Is(err, context.Canceled) && t.View.Status == "failed" {
				t.View.Result = "模型容量等待已取消，工作未发送"
			}
		}
	}
	if old := t.Requests[request]; old.Outcome == "accepted" {
		r.Outcome = "accepted"
	}
	if r.Outcome == "rejected" && t.StopPendingRequest == request {
		// This request never obtained a native Turn; its stop cannot remain
		// pending or block a later explicit continuation of the same task.
		t.StopPendingRequest, t.StopPendingTurn, t.StopPendingPhase, t.StopPendingStatus = "", "", "", ""
	}
	t.Requests[request] = r
	t.View.Outcome = r.Outcome
	// Reconcile an uncertain receipt once; normal progress is subscription-driven.
	if t.Thread != "" && err != nil && !s.childWatching[t.Thread] {
		s.childWatching[t.Thread] = true
		go s.recoverChild(s.client, s.epoch, t.Thread)
	}
	if saveErr := s.save(); saveErr != nil {
		err = saveErr
	}
	s.update()
	if r.Outcome == "rejected" && err != nil {
		err = &api.WorkMessageRejection{Cause: err}
	}
	return t.View, err
}
func (s *Session) observeTaskTurn(t *taskRecord, turn nativeTurn, history ...bool) (changed bool) {
	if len(history) == 0 && turn.Status == "inProgress" && turn.ID != "" && !slices.Contains(t.SupersededRuns, turn.ID) && (t.Run == "" || t.Run == turn.ID || turn.Status == "inProgress") {
		changed = t.AsyncAdmission.begin(turn.ID) || changed
	}
	view, pending, run, report, reportState := t.View, t.Pending, t.Run, t.ReportID, t.ReportState
	defer func() {
		changed = changed || !reflect.DeepEqual(view, t.View) || pending != t.Pending || run != t.Run || report != t.ReportID || reportState != t.ReportState
	}()
	if turn.ID == "" {
		return
	}
	for _, item := range turn.Items {
		s.observeWorkItemLocked(t, turn.ID, item, terminal(turn.Status))
		changed = s.recordWorkerAsync(t, turn.ID, item) || changed
		if item.Type == "userMessage" {
			if item.ClientID == t.StopPendingRequest && t.StopPendingTurn != turn.ID {
				t.StopPendingTurn = turn.ID
				changed = true
			}
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
		failure := (*api.TaskFailure)(nil)
		if turn.Status == "failed" {
			failure = workerFailure(t.Thread, turn.ID, turn.Error)
			previous := s.childTerminalStatus[opaque(t.Thread, turn.ID)].Failure
			if previous == nil && t.View.Failure != nil && t.View.Failure.Execution == failure.Execution {
				previous = t.View.Failure
			}
			// A retrying stream error may have recovered before a different
			// terminal failure. Only an explicit non-retrying error can fill
			// an otherwise absent terminal cause.
			if previous == nil && t.LastError != nil && t.LastError.WillRetry != nil && !*t.LastError.WillRetry {
				previous = t.LastError
			}
			if previous != nil && previous.Execution == failure.Execution {
				if turn.Error == nil {
					copy := *previous
					failure = &copy
				}
				if previous.Code == failure.Code && previous.WillRetry != nil && !*previous.WillRetry {
					failure.WillRetry = previous.WillRetry
				}
			}
			// A prior retrying stream event is not a promise to retry a terminal turn.
			if failure.WillRetry != nil && *failure.WillRetry {
				copy := *failure
				copy.WillRetry = nil
				failure = &copy
			}
		}
		s.childTerminalStatus[opaque(t.Thread, turn.ID)] = workerTerminal{Status: turn.Status, Failure: failure}
		if t.StopPendingRequest != "" && t.StopPendingTurn == turn.ID && t.StopPendingStatus != turn.Status {
			t.StopPendingStatus = turn.Status
			s.markStopTerminalSavePendingLocked("task:" + t.View.ID)
			changed = true
		}
	}
	if slices.Contains(t.SupersededRuns, turn.ID) {
		return
	}
	// A terminal fact for an earlier run cannot settle a later submission whose
	// original client identity has not appeared in this native thread snapshot.
	if t.Pending != "" || taskHasUnknownReceipt(t) {
		if turn.Status == "inProgress" {
			t.Activity = "active"
		}
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
		t.View.Failure = nil
	}
	if turn.Status == "inProgress" && !s.childTerminals[opaque(t.Thread, turn.ID)] {
		t.Activity = "active"
		t.View.Status = "working"
	}
	if terminal(turn.Status) {
		t.Activity = "idle"
		t.View.Failure = s.childTerminalStatus[opaque(t.Thread, turn.ID)].Failure
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
			if item.Type == "agentMessage" && item.Delivery != "async" {
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
	t.View.Failure = r.PriorFailure
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
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return api.Task{}, err
	}
	defer s.op.Unlock()
	s.mu.Lock()
	t, c := s.binding.Tasks[id], s.client
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("只能读取 Bot 创建的任务")
	}
	if t.Thread == "" || c == nil || s.state.Connection != "ready" {
		v := s.taskView(t)
		s.mu.Unlock()
		return v, errors.New("原任务运行环境尚未确认；保留原记录，请重新连接后重试")
	}
	threadID, epoch, revision, pending, run := t.Thread, s.epoch, s.childRevision[t.Thread], t.Pending, t.Run
	archived := s.binding.LostOwnerTasks[id]
	s.mu.Unlock()
	thread, err := readThreadState(ctx, c, threadID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch || s.closed || s.state.Connection != "ready" || s.binding.Tasks[id] != t || t.Thread != threadID || s.binding.LostOwnerTasks[id] != archived {
		return s.taskView(t), errors.New("原任务在读取期间已变化，请重试读取")
	}
	if archived {
		// The replacement connection may read the same persisted native thread.
		// It cannot resume its old owner or reconcile old effect receipts. Keep
		// the archived view/unknown state and return native read failure honestly.
		if t.Pending != pending || t.Run != run {
			return s.taskView(t), errors.New("原请求已变化，请重试读取")
		}
		return s.taskView(t), err
	}
	if s.childRevision[threadID] != revision {
		return s.taskView(t), nil
	} // The newer live projection wins.
	if t.Pending != pending || t.Run != run {
		return s.taskView(t), errors.New("原请求已变化，请重试读取")
	}
	if err != nil {
		t.Activity = ""
		return s.taskView(t), err
	}
	if thread.ID != threadID {
		t.Activity = ""
		return s.taskView(t), ErrProtocol
	}
	for _, turn := range thread.Turns {
		s.observeTaskTurn(t, turn, true)
	}
	// Activity releases capacity independently of unknown submission receipts.
	// Reading never resumes a thread, even if it is active or user-cancelled.
	if nativeTaskActive(thread) {
		t.Activity = "active"
	} else if thread.Status.Type == "idle" || thread.Status.Type == "notLoaded" {
		t.Activity = "idle"
		if t.View.Status == "working" {
			t.View.Status = "unknown"
		}
		delete(s.childRuns, threadID)
		s.scheduleChildRetirement(threadID)
	} else {
		t.Activity = ""
	}
	delete(s.childObservationFailed, threadID)
	if err := s.save(); err != nil {
		return s.taskView(t), err
	}
	s.update()
	return s.taskView(t), nil
}

func (s *Session) StopWork(ctx context.Context, id string) (api.Task, error) {
	return s.stopWork(ctx, id, nil)
}
func (s *Session) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	return s.stopWork(ctx, in.ID, &in)
}
func (s *Session) stopWork(ctx context.Context, id string, guarded *api.WorkStop) (api.Task, error) {
	capacityReceipt := false
	s.mu.Lock()
	if guarded != nil {
		t := s.binding.Tasks[id]
		if t == nil {
			s.mu.Unlock()
			return api.Task{}, errors.New("unknown Worker binding")
		}
		if done, err := s.originalOperationLocked(t, guarded.RequestID, "stop", api.WorkFingerprint(*guarded)); done || err != nil {
			v := s.taskView(t)
			s.mu.Unlock()
			return v, err
		}
		if guarded.ExecutionKey == "" || guarded.ExecutionKey != s.workExecutionKeyLocked(t) {
			v := s.taskView(t)
			s.mu.Unlock()
			return v, errors.New("original Worker execution generation changed")
		}
	}
	cancelledCapacity := false
	if t := s.binding.Tasks[id]; t != nil && !s.binding.LostOwnerTasks[id] && s.capacityThread == t.Thread && s.capacityCancel != nil {
		if s.capacityDispatchStarted && s.capacityRequest != "" {
			previous, previousTurn, previousPhase, previousStatus := t.StopPendingRequest, t.StopPendingTurn, t.StopPendingPhase, t.StopPendingStatus
			t.StopPendingRequest = s.capacityRequest
			if previous != s.capacityRequest {
				t.StopPendingTurn, t.StopPendingStatus = "", ""
			}
			t.StopPendingPhase = "requested"
			if err := s.save(); err != nil {
				t.StopPendingRequest, t.StopPendingTurn, t.StopPendingPhase, t.StopPendingStatus = previous, previousTurn, previousPhase, previousStatus
				v := s.taskView(t)
				s.mu.Unlock()
				return v, err
			}
		}
		if guarded != nil {
			outcome := "accepted"
			if s.capacityDispatchStarted {
				outcome = "unknown"
			}
			t.Requests[guarded.RequestID] = taskReceipt{Kind: "stop", IntentFingerprint: api.WorkFingerprint(*guarded), ExecutionKey: guarded.ExecutionKey, ControlTarget: s.capacityRequest, Outcome: outcome, Phase: "capacity"}
			if err := s.save(); err != nil {
				delete(t.Requests, guarded.RequestID)
				v := s.taskView(t)
				s.mu.Unlock()
				return v, err
			}
			capacityReceipt = true
		}
		s.capacityCancel()
		cancelledCapacity = true
	}
	s.mu.Unlock()
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 8*time.Second)
	defer cancel()
	s.mu.Lock()
	t := s.binding.Tasks[id]
	c := s.client
	if guarded != nil && t != nil {
		if capacityReceipt {
			r := s.reconcileControlReceiptLocked(t, guarded.RequestID)
			v := s.taskView(t)
			s.mu.Unlock()
			if r.Outcome == "accepted" {
				return v, nil
			}
			return v, errors.New("original capacity stop outcome unknown; reconcile without replay")
		}
		if done, err := s.originalOperationLocked(t, guarded.RequestID, "stop", api.WorkFingerprint(*guarded)); done || err != nil {
			v := s.taskView(t)
			s.mu.Unlock()
			return v, err
		}
		if guarded.ExecutionKey == "" || guarded.ExecutionKey != s.workExecutionKeyLocked(t) {
			v := s.taskView(t)
			s.mu.Unlock()
			return v, errors.New("original Worker execution generation changed")
		}
	}
	if t == nil {
		s.mu.Unlock()
		return api.Task{}, errors.New("只能停止 Bot 创建的任务")
	}
	if s.binding.LostOwnerTasks[id] {
		v := s.taskView(t)
		s.mu.Unlock()
		return v, errors.New("原 owner 已关闭，不能向新连接发送旧中断")
	}
	if t.StopPendingRequest != "" && (t.StopPendingStatus == "" || t.StopPendingTurn == t.Run) {
		v, status := s.taskView(t), t.StopPendingStatus
		if s.stopTerminalSavePending["task:"+t.View.ID] {
			status = ""
		}
		s.mu.Unlock()
		if status == "interrupted" {
			return v, nil
		}
		if status != "" {
			return v, errors.New("原回合已结束，但停止请求未确认及时生效；请核对实际结果")
		}
		return v, errors.New("停止请求已保留；原生中断或结果尚未确认")
	}
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.Task{}, err
	}
	run := s.childRuns[t.Thread]
	v := s.taskView(t)
	if run == "" {
		s.mu.Unlock()
		if cancelledCapacity {
			return v, nil
		}
		return v, errors.New("没有已确认的运行回合，请先读取任务")
	}
	if guarded != nil {
		t.Requests[guarded.RequestID] = taskReceipt{Kind: "stop", IntentFingerprint: api.WorkFingerprint(*guarded), ExecutionKey: guarded.ExecutionKey, Outcome: "unknown", Phase: "dispatching"}
		if err := s.save(); err != nil {
			delete(t.Requests, guarded.RequestID)
			v := s.taskView(t)
			s.mu.Unlock()
			return v, err
		}
	}
	s.mu.Unlock()
	err := callDecode(ctx, c, "turn/interrupt", map[string]any{"threadId": t.Thread, "turnId": run}, &struct{}{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if guarded != nil {
		r := t.Requests[guarded.RequestID]
		if err == nil {
			r.Outcome = "accepted"
			r.Phase = "settled"
		} else if definiteTaskRejection(err) {
			r.Outcome = "rejected"
			r.Phase = "settled"
		}
		t.Requests[guarded.RequestID] = r
		if saveErr := s.save(); saveErr != nil {
			err = saveErr
		}
	}
	return s.taskView(t), err
}

// Notices retain their origin even though the native wire accepts text input.
func (s *Session) SubmitReport(ctx context.Context, in api.Submission) (api.Receipt, error) {
	return s.submitWithSource(ctx, in, nil, true, true)
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.ReportSubmitter = (*Session)(nil)

// Legacy retired flags are ignored. Preserve identity and effect receipts; an
// unavailable cache is not a native execution result or a permission decision.
func (v *taskRecord) UnmarshalJSON(raw []byte) error {
	type plain taskRecord
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.View.Status == "unavailable" {
		decoded.View.Status = "unknown"
	}
	*v = taskRecord(decoded)
	return nil
}
