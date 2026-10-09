package caelis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type invocationKey struct{}
type workerStart struct {
	Profile       wire.ApplicationProfile `json:"profile"`
	Prompt        string                  `json:"prompt"`
	Authorization string                  `json:"authorization"`
}

// The latest outbound prompt owns the Worker generation until its original
// receipt and native turn have both been observed. The preceding turn is only
// history; it cannot confirm or free a later prompt.
type workerSubmission struct {
	OperationID    string   `json:"operationID"`
	BeforePromptID string   `json:"beforePromptID,omitempty"`
	BeforeTurn     string   `json:"beforeTurn,omitempty"`
	BeforeTask     api.Task `json:"beforeTask"`
	Steering       bool     `json:"steering,omitempty"`
}
type worker struct {
	Native      bool                    `json:"native,omitempty"`
	Binding     wire.ApplicationBinding `json:"binding"`
	Task        api.Task                `json:"task"`
	Fingerprint string                  `json:"fingerprint"`
	PromptID    string                  `json:"promptID"`
	Stopped     bool                    `json:"stopped"`
	Start       *workerStart            `json:"start,omitempty"`
	Submission  *workerSubmission       `json:"submission,omitempty"`
	Retired     bool                    `json:"retired,omitempty"`
	Activity    string                  `json:"-"`
}

func (s *Session) authority(ctx context.Context) (wire.ApplicationCall, error) {
	call, ok := ctx.Value(invocationKey{}).(wire.ApplicationCall)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok || !s.connected || s.closed || call.SessionId != s.state.Session.SessionId || call.ApplicationId != s.state.Connection.ApplicationId || call.ConnectionId != s.state.Connection.ConnectionId || call.PrincipalId != s.state.PrincipalID {
		return call, errors.New("需要当前助手请求的明确授权来源")
	}
	if call.Source.Kind != "user" && call.Source.Kind != "authorized_background" {
		return call, errors.New("应用摘要或外部材料不能授权新工作")
	}
	return call, nil
}
func (s *Session) WorkAdmission(ctx context.Context) error { _, e := s.authority(ctx); return e }
func (s *Session) WorkStates() []api.WorkState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []api.WorkState{}
	for _, w := range s.state.Workers {
		v := s.workerViewLocked(w)
		key := ""
		if p := s.state.Views[w.Binding.SessionId]; p != nil && !w.Retired {
			key = observedTurn(p)
		}
		activity := w.Activity
		if !s.connected {
			activity = ""
		}
		if p := s.state.Views[w.Binding.SessionId]; p != nil && !w.Retired && s.connected {
			if workerNativeActive(p.State) {
				activity = "active"
			} else if p.State.Run.Active != nil && activity != "active" {
				activity = "idle"
			}
		}
		out = append(out, api.WorkState{Task: v, Activity: activity, ExecutionKey: key, StopRequested: w.Stopped, StartFingerprint: w.Fingerprint})
	}
	return out
}
func workerNativeActive(state wire.SessionState) bool {
	if value(state.Run.Active) || value(state.Run.WaitingApproval) || state.Approval.Active != nil {
		return true
	}
	switch value(state.Run.Status) {
	case "working", "running", "pending", "waiting_approval", "awaiting_approval":
		return true
	}
	return false
}
func (s *Session) workerViewLocked(w worker) api.Task {
	out := w.Task
	if w.Retired {
		out.Status = "unavailable"
		return out
	}
	if w.Start != nil {
		out.Status, out.Outcome, out.Result = "unknown", "unknown", ""
		return out
	}
	p := s.state.Views[w.Binding.SessionId]
	currentTurn := observedTurn(p)
	receipt, recorded := s.state.Operations[w.PromptID]
	if out.Status == "failed" && out.Outcome == "rejected" && (w.Binding.SessionId == "" || recorded && (receipt.Outcome == "rejected" || receipt.Outcome == "conflicted")) {
		return out
	}
	operationTurn := receipt.TurnID
	if operationTurn == "" && p != nil {
		for _, item := range p.Items {
			if item.Kind == "user" && item.RequestID == w.PromptID && item.TurnKey != "" {
				operationTurn = item.TurnKey
			}
		}
	}
	if recorded && receipt.Outcome == "unknown" {
		out.Status, out.Outcome, out.Result = "unknown", "unknown", ""
		return out
	}
	if w.Submission != nil && w.Submission.OperationID == w.PromptID {
		submission := w.Submission
		if recorded && (receipt.Outcome == "rejected" || receipt.Outcome == "conflicted") {
			out = submission.BeforeTask
			out.Outcome = "rejected"
		} else if !recorded || receipt.Outcome == "unknown" {
			out.Status, out.Outcome, out.Result = "unknown", "unknown", ""
			return out
		} else if !submission.Steering && (operationTurn == "" || currentTurn != operationTurn || currentTurn == submission.BeforeTurn) {
			out.Status, out.Outcome, out.Result = "pending", "accepted", ""
			return out
		} else if submission.Steering {
			observedInput := false
			if p != nil && currentTurn == submission.BeforeTurn && (operationTurn == "" || operationTurn == submission.BeforeTurn) {
				for _, item := range p.Items {
					if item.Kind == "user" && item.RequestID == w.PromptID && item.TurnKey == submission.BeforeTurn {
						observedInput = true
						break
					}
				}
			}
			if !observedInput {
				out.Status, out.Outcome, out.Result = "pending", "accepted", ""
				return out
			}
		}
	} else if strings.HasPrefix(w.PromptID, "work-send-") && w.Task.Outcome == "unknown" {
		// A legacy continuation has no saved preceding-turn identity. Only an
		// exact accepted receipt with a matching native target can clear it.
		if receipt.Outcome == "rejected" || receipt.Outcome == "conflicted" {
			out.Outcome = "rejected"
		} else if !recorded || receipt.Outcome != "accepted" && receipt.Outcome != "committed" || operationTurn == "" || currentTurn != operationTurn {
			out.Status, out.Outcome, out.Result = "unknown", "unknown", ""
			return out
		}
	}
	if p != nil {
		status := value(p.State.Run.Status)
		if value(p.State.Run.Active) {
			out.Status = "working"
		} else if status != "" {
			out.Status = status
		}
		if status == "cancelled" || status == "stopped" {
			out.Status = "interrupted"
		}
		if p.State.Approval.Active != nil && !s.automaticApprovalLocked(w.Binding.SessionId, p.State.Approval.Active) {
			out.Status = "waiting_approval"
		}
		var result []string
		turn := observedTurn(p)
		for _, i := range p.Items {
			if i.Kind == "assistant" && (turn == "" || i.TurnKey == turn) {
				result = append(result, i.Text)
			}
		}
		out.Result = strings.Join(result, "\n")
		if len(out.Result) > 16000 {
			out.Result = out.Result[:16000]
		}
	}
	return out
}
func (s *Session) workerNeedsReconcileLocked(w worker) bool {
	return !w.Retired && s.workerViewLocked(w).Status == "unknown"
}
func sameWorkerOwner(a, b worker) bool {
	return a.Task.ID == b.Task.ID && a.Task.Workspace == b.Task.Workspace && a.Native == b.Native && a.Fingerprint == b.Fingerprint && a.PromptID == b.PromptID &&
		reflect.DeepEqual(a.Submission, b.Submission) &&
		a.Binding.SessionId == b.Binding.SessionId && a.Binding.ApplicationId == b.Binding.ApplicationId && a.Binding.ConnectionId == b.Binding.ConnectionId && a.Binding.PrincipalId == b.Binding.PrincipalId && a.Binding.CreationDigest == b.Binding.CreationDigest && a.Binding.Archived == b.Binding.Archived
}
func (s *Session) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	call, e := s.authority(ctx)
	if e != nil {
		return api.Task{}, e
	}
	if in.ID == "" || in.RequestID == "" || !filepath.IsAbs(in.Workspace) {
		return api.Task{}, errors.New("工作配置无效")
	}
	s.step.Lock()
	defer s.step.Unlock()
	fp := digest([]byte(in.Title + "\x00" + in.Prompt))[:32]
	if in.TaskStart.Workspace != "" {
		fp = digest([]byte(in.Title + "\x00" + in.Prompt + "\x00" + in.TaskStart.Workspace))[:32]
	}
	s.mu.Lock()
	old, exists := s.state.Workers[in.ID]
	execution := s.workExecution
	s.mu.Unlock()
	if exists {
		if old.Fingerprint != fp || old.Task.Workspace != in.Workspace {
			return api.Task{}, errors.New("任务请求冲突")
		}
		s.mu.Lock()
		v := s.workerViewLocked(old)
		s.mu.Unlock()
		return v, nil
	}
	if in.TaskStart.Workspace != "" {
		resolved, err := api.ResolveTaskWorkspace(in.Workspace)
		if err != nil {
			return api.Task{}, err
		}
		if resolved != in.Workspace {
			return api.Task{}, errors.New("工作目录已发生重定向")
		}
	}
	profile := wire.ApplicationProfile{Model: execution.Model, ReasoningEffort: pointer(execution.Effort), ServiceTier: pointer(execution.ServiceTier)}
	w := worker{Native: true, Task: api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "unknown", Outcome: "unknown"}, Fingerprint: fp, PromptID: "work-prompt-" + digest([]byte(in.RequestID)), Start: &workerStart{Profile: profile, Prompt: in.Prompt, Authorization: call.Source.OperationId}}
	s.mu.Lock()
	s.state.Workers[in.ID] = w
	e = s.saveLocked()
	s.mu.Unlock()
	if e != nil {
		return w.Task, e
	}
	out, e := s.advanceWorker(ctx, w)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return out, e
}

// Resume only a persisted, authorized start. command() reads an uncertain
// operation and never re-dispatches it. Later steps are issued only after the
// preceding receipt is known, including after a process crash.
func (s *Session) advanceWorker(ctx context.Context, w worker) (api.Task, error) {
	if w.Start == nil {
		return w.Task, nil
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	op := "work-create-" + digest([]byte(w.Task.ID))
	if w.Binding.SessionId == "" {
		path := "/application/sessions"
		var request any = wire.CreateApplicationSessionRequest{OperationId: &op, Profile: w.Start.Profile}
		if w.Native {
			path = "/application/workers"
			request = wire.CreateWorkerRequest{OperationId: &op, Cwd: w.Task.Workspace, Title: &w.Task.Title, Model: &w.Start.Profile.Model, ReasoningEffort: w.Start.Profile.ReasoningEffort, FastMode: pointer(value(w.Start.Profile.ServiceTier) == "priority")}
		}
		result, e := s.command(ctx, op, path, request)
		if e != nil || !succeeded(result.Outcome) {
			if result.Outcome == "rejected" || result.Outcome == "conflicted" {
				w.Task.Status, w.Task.Outcome, w.Start = "failed", "rejected", nil
				return w.Task, errors.Join(e, s.saveWorker(w))
			}
			return w.Task, errors.Join(e, errors.New("独立任务创建未确认"))
		}
		sid := value(result.SessionId)
		if sid == "" && result.Resource != nil {
			sid = value(result.Resource.Ref)
		}
		if w.Native {
			var workers []wire.ApplicationWorker
			if e = c.json(ctx, "GET", "/application/workers", nil, &workers, "", ""); e != nil {
				return w.Task, e
			}
			for _, grant := range workers {
				if grant.SessionId == sid {
					w.Binding = wire.ApplicationBinding{SessionId: sid, ApplicationId: grant.ApplicationId, ConnectionId: grant.ConnectionId, PrincipalId: grant.PrincipalId}
					break
				}
			}
		} else if e = c.json(ctx, "GET", "/application/sessions/"+idPath(sid), nil, &w.Binding, "", ""); e != nil {
			return w.Task, e
		}
		s.mu.Lock()
		life := s.state.Connection
		s.mu.Unlock()
		if w.Binding.SessionId != sid || w.Binding.ApplicationId != life.ApplicationId || w.Binding.ConnectionId != life.ConnectionId || w.Binding.PrincipalId != life.PrincipalId {
			return w.Task, errors.New("独立任务绑定不匹配")
		}
		if e = s.saveWorker(w); e != nil {
			return w.Task, e
		}
	}
	sid := w.Binding.SessionId
	var result wire.CommandResult
	var e error
	if w.Native {
		result, e = s.command(ctx, w.PromptID, "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &w.PromptID, SessionId: &sid, Input: &w.Start.Prompt})
	} else {
		grant, grantErr := s.createGrant(ctx, sid, "work-"+w.Task.ID, w.Start.Authorization)
		if grantErr != nil {
			return w.Task, grantErr
		}
		result, e = s.command(ctx, w.PromptID, "/application/sessions/"+idPath(sid)+"/prompt", wire.ApplicationPromptRequest{OperationId: &w.PromptID, SessionId: &sid, SourceKind: "authorized_background", GrantId: &grant.Id, Input: &w.Start.Prompt})
	}
	w.Task.Outcome = productOutcome(result.Outcome)
	if succeeded(result.Outcome) {
		w.Task.Status, w.Start = "pending", nil
	} else if result.Outcome == "rejected" || result.Outcome == "conflicted" {
		w.Task.Status, w.Start = "failed", nil
	}
	save := s.saveWorker(w)
	return w.Task, errors.Join(e, save)
}
func (s *Session) saveWorker(w worker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Workers[w.Task.ID] = w
	e := s.saveLocked()
	s.bumpLocked()
	return e
}

func (s *Session) ReadWork(ctx context.Context, id string) (api.Task, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	w, ok := s.state.Workers[id]
	if !ok {
		s.mu.Unlock()
		return api.Task{}, errors.New("任务不属于当前 Bot")
	}
	// A durable prepared intent without a journal could not have dispatched.
	// The command saves its journal before POST, so only this case may roll
	// back without an original-owner read.
	pending := w.Submission
	_, journalRecorded := s.state.Operations[w.PromptID]
	if pending != nil && !journalRecorded {
		previous := w
		w.Task, w.PromptID, w.Submission = pending.BeforeTask, pending.BeforePromptID, nil
		s.state.Workers[id] = w
		if err := s.saveLocked(); err != nil {
			s.state.Workers[id] = previous
			v := s.workerViewLocked(previous)
			s.mu.Unlock()
			return v, err
		}
	}
	if strings.HasPrefix(w.PromptID, "work-send-") && s.state.Operations[w.PromptID].Outcome == "unknown" {
		op := w.PromptID
		s.mu.Unlock()
		if err := s.recoverOperation(ctx, op); err != nil {
			return api.Task{ID: id, Status: "unknown", Outcome: "unknown"}, err
		}
		s.mu.Lock()
		w = s.state.Workers[id]
		if w.PromptID != op {
			v := s.workerViewLocked(w)
			s.mu.Unlock()
			return v, errors.New("original worker receipt changed during reconciliation")
		}
		if s.state.Operations[op].Outcome == "unknown" {
			v := s.workerViewLocked(w)
			s.mu.Unlock()
			return v, errors.New("original continuation receipt remains unknown")
		}
	}
	projected := s.workerViewLocked(w)
	needsReconcile := s.workerNeedsReconcileLocked(w)
	receipt := s.state.Operations[w.PromptID]
	needsTurn := w.Submission != nil && projected.Status == "pending" || w.Submission == nil && strings.HasPrefix(w.PromptID, "work-send-") && projected.Status == "unknown" && (receipt.Outcome == "accepted" || receipt.Outcome == "committed") && receipt.TurnID != ""
	if !needsReconcile && !needsTurn {
		v := s.workerViewLocked(w)
		s.mu.Unlock()
		return v, nil
	}
	if w.Start != nil && needsReconcile {
		v := s.workerViewLocked(w)
		s.mu.Unlock()
		return v, errors.New("original worker start receipt still needs reconciliation")
	}
	c, sid, generation, instance, connected := s.client, w.Binding.SessionId, s.generation, s.state.InstanceID, s.connected
	before := s.state.Views[sid]
	var observed uint64
	var expectedTurn string
	var previous []byte
	if before != nil {
		observed = before.Observed
		expectedTurn = value(before.State.Run.TurnId)
		var err error
		previous, err = json.Marshal(before.State)
		if err != nil {
			s.mu.Unlock()
			return projected, err
		}
	}
	s.mu.Unlock()
	if c == nil || sid == "" || !connected {
		return projected, errors.New("original worker binding unconfirmed")
	}
	var state wire.SessionState
	if err := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); err != nil {
		return projected, err
	}
	if state.SessionId != sid {
		return projected, errors.New("original worker state mismatch")
	}
	if expectedTurn != "" && value(state.Run.TurnId) != expectedTurn && !needsTurn {
		return projected, errors.New("original worker turn is not the current projected turn")
	}
	if state.Run.Active == nil && !workerNativeActive(state) {
		return projected, errors.New("original worker activity unconfirmed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.state.Workers[id]
	if !exists {
		return api.Task{}, errors.New("original worker binding changed")
	}
	p := s.state.Views[sid]
	var currentState []byte
	if p != nil {
		var err error
		currentState, err = json.Marshal(p.State)
		if err != nil {
			return s.workerViewLocked(current), err
		}
	}
	if s.client != c || !s.connected || s.closed || s.generation != generation || s.state.InstanceID != instance || !sameWorkerOwner(w, current) || current.Start != nil || s.workerViewLocked(current).Status != projected.Status || p != before || p != nil && (p.Observed != observed || !bytes.Equal(currentState, previous)) {
		return s.workerViewLocked(current), errors.New("original worker or projection changed; retirement refused")
	}
	if needsTurn {
		created := p == nil
		if p == nil {
			p = &view{Items: []api.Item{}, Seen: map[string]bool{}}
			s.state.Views[sid] = p
		}
		prior := p.State
		p.State = state
		p.Observed++
		current.Activity = "idle"
		if workerNativeActive(state) {
			current.Activity = "active"
		} else if state.Run.Active != nil && (s.workerViewLocked(current).Status == "unknown" && (current.Submission != nil && !current.Submission.Steering && current.Submission.BeforeTurn != value(state.Run.TurnId) || current.Submission == nil && receipt.TurnID == value(state.Run.TurnId)) ||
			s.workerViewLocked(current).Status == "pending" && current.Submission != nil && current.Submission.Steering && current.Submission.BeforeTurn == value(state.Run.TurnId) && (receipt.Outcome == "accepted" || receipt.Outcome == "committed")) {
			current.Retired = true
			current.Task.Status = "unavailable"
		}
		s.state.Workers[id] = current
		if err := s.saveLocked(); err != nil {
			if created {
				delete(s.state.Views, sid)
			} else {
				p.State = prior
				p.Observed--
			}
			s.state.Workers[id] = w
			return projected, err
		}
		return s.workerViewLocked(current), nil
	}
	if strings.HasPrefix(current.PromptID, "work-send-") {
		receipt := s.state.Operations[current.PromptID]
		if receipt.Outcome == "unknown" || receipt.Outcome == "accepted" || receipt.Outcome == "committed" {
			if current.Submission == nil && (receipt.TurnID == "" || receipt.TurnID != value(state.Run.TurnId)) || current.Submission != nil && !current.Submission.Steering && current.Submission.BeforeTurn == value(state.Run.TurnId) || receipt.TurnID != "" && receipt.TurnID != value(state.Run.TurnId) {
				return s.workerViewLocked(current), errors.New("original continuation generation is not confirmed idle")
			}
		}
	}
	previousWorker := current
	if workerNativeActive(state) {
		current.Activity = "active"
	} else {
		current.Activity = "idle"
		current.Retired = true
		current.Task.Status = "unavailable"
	}
	s.state.Workers[id] = current
	if err := s.saveLocked(); err != nil {
		s.state.Workers[id] = previousWorker
		return s.workerViewLocked(previousWorker), err
	}
	return s.workerViewLocked(current), nil
}

func (s *Session) RetireWork(ctx context.Context, id string) (api.Task, error) {
	v, err := s.ReadWork(ctx, id)
	if err != nil || v.Status == "unavailable" {
		return v, err
	}
	return v, errors.New("original worker is active; retirement refused")
}
func (s *Session) WorkMessageRecorded(in api.TaskMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.state.Workers[in.ID]
	op := "work-send-" + digest([]byte(in.RequestID))
	if !ok || w.PromptID != op {
		return false
	}
	_, ok = s.state.Operations[op]
	return ok
}

func (s *Session) beginWorkerSubmission(w worker, op string, steer bool) (worker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := w
	before := s.workerViewLocked(w)
	w.Submission = &workerSubmission{OperationID: op, BeforePromptID: w.PromptID, BeforeTurn: observedTurn(s.state.Views[w.Binding.SessionId]), BeforeTask: before, Steering: steer}
	w.PromptID = op
	w.Task.Status, w.Task.Outcome, w.Task.Result = "unknown", "unknown", ""
	s.state.Workers[w.Task.ID] = w
	if err := s.saveLocked(); err != nil {
		s.state.Workers[w.Task.ID] = previous
		return previous, err
	}
	s.bumpLocked()
	return w, nil
}

func (s *Session) finishWorkerSubmission(w worker, result wire.CommandResult, callErr error) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := w
	switch result.Outcome {
	case "rejected", "conflicted":
		w.Task = w.Submission.BeforeTask
		w.Task.Outcome = "rejected"
		w.Submission = nil
	case "accepted", "committed":
		w.Task.Status, w.Task.Outcome, w.Stopped = "pending", "accepted", false
	default:
		w.Task.Status, w.Task.Outcome = "unknown", "unknown"
	}
	s.state.Workers[w.Task.ID] = w
	if err := s.saveLocked(); err != nil {
		s.state.Workers[w.Task.ID] = previous
		return s.workerViewLocked(previous), errors.Join(callErr, err)
	}
	s.bumpLocked()
	return s.workerViewLocked(w), callErr
}

func (s *Session) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	call, e := s.authority(ctx)
	if e != nil {
		return api.Task{}, e
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	w, ok := s.state.Workers[in.ID]
	v := s.state.Views[w.Binding.SessionId]
	op := "work-send-" + digest([]byte(in.RequestID))
	_, retry := s.state.Operations[op]
	busy := v != nil && (value(v.State.Run.Active) || v.State.Approval.Active != nil)
	projected := s.workerViewLocked(w)
	unresolved := projected.Status == "unknown" || projected.Status == "pending" || w.Start != nil
	s.mu.Unlock()
	if !ok || w.Binding.SessionId == "" || v == nil {
		return api.Task{}, errors.New("任务未就绪")
	}
	sid := w.Binding.SessionId
	if retry && w.PromptID == op && w.Native {
		res, err := s.submitNativeInput(ctx, sid, op, in.Prompt, nil, busy)
		projected.Outcome = productOutcome(res.Outcome)
		if res.Outcome == "unknown" && err == nil {
			err = errors.New("original request receipt remains unknown")
		}
		return projected, err
	}
	if unresolved || w.Retired {
		return projected, errors.New("task retired or original receipt unconfirmed; continuation refused")
	}
	if retry {
		return projected, errors.New("original request receipt already exists; read the exact request before continuing")
	}
	if w.Native || s.isSteeringRetry(op) || busy && !retry {
		w, e = s.beginWorkerSubmission(w, op, busy)
		if e != nil {
			return projected, e
		}
		res, err := s.submitNativeInput(ctx, sid, op, in.Prompt, nil, busy)
		return s.finishWorkerSubmission(w, res, err)
	}
	g, e := s.createGrant(ctx, sid, "continue-"+digest([]byte(in.RequestID)), call.Source.OperationId)
	if e != nil {
		return w.Task, e
	}
	w, e = s.beginWorkerSubmission(w, op, false)
	if e != nil {
		return projected, e
	}
	res, e := s.command(ctx, op, "/application/sessions/"+idPath(sid)+"/prompt", wire.ApplicationPromptRequest{OperationId: &op, SessionId: &sid, SourceKind: "authorized_background", GrantId: &g.Id, Input: &in.Prompt})
	return s.finishWorkerSubmission(w, res, e)
}
func (s *Session) StopWork(ctx context.Context, id string) (api.Task, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	w, ok := s.state.Workers[id]
	s.mu.Unlock()
	if !ok {
		return api.Task{}, errors.New("任务不属于当前 Bot")
	}
	if e := s.interruptSession(ctx, w.Binding.SessionId); e != nil {
		return w.Task, e
	}
	s.mu.Lock()
	w.Stopped = true
	s.state.Workers[id] = w
	e := s.saveLocked()
	out := s.workerViewLocked(w)
	s.mu.Unlock()
	return out, e
}
func (s *Session) interruptSession(ctx context.Context, sid string) error {
	s.mu.Lock()
	c := s.client
	instance := s.state.InstanceID
	s.mu.Unlock()
	var state wire.SessionState
	if e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); e != nil {
		return e
	}
	if state.SessionId != sid || !value(state.Run.Active) {
		return errors.New("当前没有可停止的任务")
	}
	target := wire.TurnTarget{HandleId: value(state.Run.HandleId), RunId: value(state.Run.RunId), TurnId: value(state.Run.TurnId)}
	raw, _ := json.Marshal(target)
	op := "cancel-" + digest(append([]byte(instance+sid), raw...))
	res, e := s.command(ctx, op, "/sessions/"+idPath(sid)+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	if e != nil {
		return e
	}
	if !succeeded(res.Outcome) {
		return errors.New("取消结果尚未确认")
	}
	return nil
}

func (s *Session) refreshWorkers(ctx context.Context, c *client) error {
	s.mu.Lock()
	workers := clone(s.state.Workers)
	s.mu.Unlock()
	var failures error
	for id, w := range workers {
		if w.Retired {
			continue
		}
		if w.Start != nil {
			// Unknown worker admission must not disconnect an otherwise usable
			// resident assistant. Its durable state remains visible as unknown.
			_, _ = s.advanceWorker(ctx, w)
			s.mu.Lock()
			w = s.state.Workers[id]
			s.mu.Unlock()
		}
		sid := w.Binding.SessionId
		if sid == "" {
			continue
		}
		s.mu.Lock()
		before := s.state.Views[sid]
		var observed, approvalVersion uint64
		if before != nil {
			observed = before.Observed
			approvalVersion = before.ApprovalVersion
		}
		s.mu.Unlock()
		var v wire.SessionState
		if e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &v, "", ""); e != nil {
			failures = errors.Join(failures, e)
			continue
		}
		if v.SessionId != sid {
			failures = errors.Join(failures, errors.New("工作状态来源不匹配"))
			continue
		}
		s.mu.Lock()
		p := s.state.Views[sid]
		if p == nil {
			p = &view{Items: []api.Item{}, Seen: map[string]bool{}}
			s.state.Views[sid] = p
		}
		if p == before && p.Observed == observed || before == nil && p.Observed == 0 {
			approval := p.State.Approval
			p.State = v
			p.State.Approval = approval
		}
		reconcileApproval(p, before, approvalVersion, v)
		s.ensureStreamLocked(sid)
		s.bumpLocked()
		e := s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			failures = errors.Join(failures, e)
		}
	}
	return failures
}
