package caelis

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

type invocationKey struct{}
type workerStart struct {
	Profile       wire.SessionProfile `json:"profile"`
	Prompt        string              `json:"prompt"`
	Authorization string              `json:"authorization"`
	Files         []workerInput       `json:"files,omitempty"`
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
	// Frozen before native admission; absent on local/legacy bindings.
	NativeScope string              `json:"nativeScope,omitempty"`
	HostStart   *workIntent         `json:"hostStart,omitempty"`
	Native      bool                `json:"native,omitempty"`
	Binding     wire.SessionBinding `json:"binding"`
	Task        api.Task            `json:"task"`
	Fingerprint string              `json:"fingerprint"`
	PromptID    string              `json:"promptID"`
	Stopped     bool                `json:"stopped"`
	Start       *workerStart        `json:"start,omitempty"`
	Submission  *workerSubmission   `json:"submission,omitempty"`
	Activity    string              `json:"-"`
}

func (s *Session) authority(ctx context.Context) (wire.SessionToolCall, error) {
	call, ok := ctx.Value(invocationKey{}).(wire.SessionToolCall)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok || call.SessionId != s.state.Session.SessionId || call.PrincipalId != s.state.PrincipalID {
		return call, &api.WorkAdmissionError{Code: "source_not_authorized", Cause: errors.New("需要当前助手请求的明确授权来源")}
	}
	if !s.connected || s.closed {
		return call, errors.New("需要当前助手请求的明确授权来源")
	}
	if call.Source.Kind != "user" && call.Source.Kind != "authorized_background" {
		return call, &api.WorkAdmissionError{Code: "source_not_authorized", Cause: errors.New("应用摘要或外部材料不能授权新工作")}
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
		key, stopKey := "", ""
		if p := s.state.Views[w.Binding.SessionId]; p != nil {
			key = observedTurn(p)
			stopKey = workStopExecutionKey(p.State)
		}
		activity := w.Activity
		if !s.connected {
			activity = ""
		}
		out = append(out, api.WorkState{Task: v, Activity: activity, ExecutionKey: key, StopExecutionKey: stopKey, StopRequested: w.Stopped, StopAcknowledged: s.workerStopAcknowledged(w, stopKey), StartFingerprint: w.Fingerprint})
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
	out.Failure = nil
	if w.Start != nil {
		out.Status, out.Outcome, out.Result = "unknown", "unknown", ""
		return out
	}
	p := s.state.Views[w.Binding.SessionId]
	currentTurn := observedTurn(p)
	receipt, recorded := s.state.Operations[w.PromptID]
	if out.Status == "failed" && (out.Outcome == "rejected" || out.Outcome == "conflicted") && (w.Binding.SessionId == "" || recorded && (receipt.Outcome == "rejected" || receipt.Outcome == "conflicted")) {
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
			out.Outcome = receipt.Outcome
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
			out.Outcome = receipt.Outcome
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
		if out.Status == "failed" {
			out.Failure = api.UnknownTaskFailure("caelis", api.WorkFingerprint([]string{s.state.StoreID, w.Binding.SessionId, turn}))
		}
		out.Result = strings.Join(result, "\n")
		if len(out.Result) > 16000 {
			out.Result = out.Result[:16000]
		}
	}
	return out
}
func sameWorkerOwner(a, b worker) bool {
	return a.Task.ID == b.Task.ID && a.Task.Workspace == b.Task.Workspace && a.Native == b.Native && a.NativeScope == b.NativeScope && a.Fingerprint == b.Fingerprint && a.PromptID == b.PromptID &&
		reflect.DeepEqual(a.Submission, b.Submission) &&
		a.Binding.SessionId == b.Binding.SessionId && a.Binding.PrincipalId == b.Binding.PrincipalId && a.Binding.CreationDigest == b.Binding.CreationDigest && a.Binding.Archived == b.Binding.Archived
}

// Native operations are principal-global in Core. Each new foreign binding
// therefore freezes its own source-qualified domain; original host IDs remain
// in WorkIntent. Empty scope retains local/legacy receipt derivation unchanged.
func (w worker) operationID(kind, id string) string {
	if w.NativeScope != "" {
		id = w.NativeScope + "\x00" + w.Task.ID + "\x00" + id
	}
	return "work-" + kind + "-" + digest([]byte(id))
}
func (s *Session) workerWriteAllowed(w worker) error {
	if s.workSourceIdentity != "" && w.NativeScope == "" {
		return errors.New("original_worker_source_scope_unavailable")
	}
	return nil
}
func (s *Session) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	call, e := s.authority(ctx)
	if e != nil {
		return api.Task{}, e
	}
	if in.ID == "" || in.RequestID == "" || !filepath.IsAbs(in.Workspace) {
		return api.Task{}, errors.New("工作配置无效")
	}
	inputs, e := readWorkerInputs(in.Files)
	if e != nil {
		return api.Task{}, e
	}
	digests := workerInputDigests(inputs)
	if len(in.FileDigests) != 0 && !slices.Equal(in.FileDigests, digests) {
		return api.Task{}, errors.New("Worker input digest differs")
	}
	in.FileDigests = digests
	hostIntent := &workIntent{in.ID, in.RequestID, "start", api.WorkFingerprint(in)}
	s.step.Lock()
	defer s.step.Unlock()
	fp := digest([]byte(in.Title + "\x00" + in.Prompt))[:32]
	if in.TaskStart.Workspace != "" {
		fp = digest([]byte(in.Title + "\x00" + in.Prompt + "\x00" + in.TaskStart.Workspace))[:32]
	}
	s.mu.Lock()
	old, exists := s.state.Workers[in.ID]
	execution := s.workExecution
	if in.ExecutionOverride != nil {
		execution = *in.ExecutionOverride
	}
	s.mu.Unlock()
	if exists {
		if old.Fingerprint != fp || old.Task.Workspace != in.Workspace || old.HostStart != nil && *old.HostStart != *hostIntent || old.HostStart == nil && len(inputs) != 0 {
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
	profile := wire.SessionProfile{Model: execution.Model, ReasoningEffort: pointer(execution.Effort), ServiceTier: pointer(execution.ServiceTier)}
	w := worker{HostStart: hostIntent, Native: true, Task: api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "unknown", Outcome: "unknown"}, Fingerprint: fp, Start: &workerStart{Profile: profile, Prompt: in.Prompt, Authorization: call.Source.OperationId, Files: inputs}}
	if s.workSourceIdentity != "" {
		w.NativeScope = digest([]byte(s.workSourceIdentity + "\x00" + rand.Text()))
	}
	w.PromptID = w.operationID("prompt", in.RequestID)
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
	if err := s.workerWriteAllowed(w); err != nil {
		return w.Task, err
	}
	if w.HostStart != nil {
		ctx = context.WithValue(ctx, workIntentKey{}, w.HostStart)
	}
	s.mu.Lock()
	c := s.client
	op := w.operationID("create", w.Task.ID)
	_, createRecorded := s.state.Operations[op]
	s.mu.Unlock()
	if w.Binding.SessionId == "" {
		path := "/sessions"
		var request any = wire.CreateSessionRequest{OperationId: &op, Profile: &w.Start.Profile}
		if w.Native {
			path = "/sessions"
			request = wire.CreateSessionRequest{OperationId: &op, Cwd: &w.Task.Workspace, Title: &w.Task.Title, Model: &w.Start.Profile.Model, ReasoningEffort: w.Start.Profile.ReasoningEffort, FastMode: pointer(value(w.Start.Profile.ServiceTier) == "priority")}
		}
		result, e := s.command(ctx, op, path, request)
		if e != nil || !succeeded(result.Outcome) {
			if result.Outcome == "rejected" || result.Outcome == "conflicted" {
				w.Task.Status, w.Task.Outcome, w.Start = "failed", string(result.Outcome), nil
				save := s.saveWorker(w)
				if save == nil && !createRecorded && result.OperationId == op {
					e = nativeWorkerRefusal(result, e)
				}
				return w.Task, errors.Join(e, save)
			}
			return w.Task, errors.Join(e, errors.New("独立任务创建未确认"))
		}
		sid := value(result.SessionId)
		if sid == "" && result.Resource != nil {
			sid = value(result.Resource.Ref)
		}
		s.mu.Lock()
		principal := s.state.PrincipalID
		s.mu.Unlock()
		if w.Native {
			var state wire.SessionState
			if e = c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); e != nil {
				return w.Task, e
			}
			if state.SessionId != sid {
				return w.Task, errors.New("独立任务状态不匹配")
			}
			w.Binding = wire.SessionBinding{SessionId: sid, PrincipalId: principal}
		} else if e = c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/profile", nil, &w.Binding, "", ""); e != nil {
			return w.Task, e
		}
		if w.Binding.SessionId != sid || w.Binding.PrincipalId != principal {
			return w.Task, errors.New("独立任务绑定不匹配")
		}
		if e = s.saveWorker(w); e != nil {
			return w.Task, e
		}
	}
	sid := w.Binding.SessionId
	var result wire.CommandResult
	parts, e := s.encodeWorkerInputs(ctx, sid, w.PromptID, w.Start.Files)
	if e != nil {
		return w.Task, e
	}
	s.mu.Lock()
	_, promptRecorded := s.state.Operations[w.PromptID]
	s.mu.Unlock()
	if w.Native {
		result, e = s.command(ctx, w.PromptID, "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &w.PromptID, SessionId: &sid, Input: &w.Start.Prompt, ContentParts: parts})
	} else {
		grant, grantErr := s.createGrant(ctx, sid, "work-"+w.Task.ID, w.Start.Authorization)
		if grantErr != nil {
			return w.Task, grantErr
		}
		result, e = s.command(ctx, w.PromptID, "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &w.PromptID, SessionId: &sid, SourceKind: pointer("authorized_background"), GrantId: &grant.Id, Input: &w.Start.Prompt, ContentParts: parts})
	}
	w.Task.Outcome = productOutcome(result.Outcome)
	if succeeded(result.Outcome) {
		w.Task.Status, w.Start = "pending", nil
	} else if result.Outcome == "rejected" || result.Outcome == "conflicted" {
		w.Task.Status, w.Task.Outcome, w.Start = "failed", string(result.Outcome), nil
	}
	save := s.saveWorker(w)
	if save == nil && !promptRecorded && result.OperationId == w.PromptID && (result.Outcome == "rejected" || result.Outcome == "conflicted") {
		e = nativeWorkerRefusal(result, e)
	}
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
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.step); err != nil {
		return api.Task{}, err
	}
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
		_ = s.recoverOperation(ctx, op) // Failure keeps the original unknown journal; never re-POST.
		s.mu.Lock()
		w = s.state.Workers[id]
		if w.PromptID != op {
			v := s.workerViewLocked(w)
			s.mu.Unlock()
			return v, errors.New("original worker receipt changed during reconciliation")
		}
	}
	projected := s.workerViewLocked(w)
	if w.Start != nil {
		s.mu.Unlock()
		return projected, errors.New("original worker start receipt still needs reconciliation")
	}
	w.Activity = "" // A failed or mismatched read cannot retain an old idle proof.
	s.state.Workers[id] = w
	c, sid, generation, instance, connected := s.client, w.Binding.SessionId, s.generation, s.state.InstanceID, s.connected
	before := s.state.Views[sid]
	var observed uint64
	var previous []byte
	if before != nil {
		observed = before.Observed
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
		s.mu.Lock()
		if current, ok := s.state.Workers[id]; ok && s.client == c && s.generation == generation && sameWorkerOwner(w, current) {
			current.Activity = ""
			s.state.Workers[id] = current
		}
		s.mu.Unlock()
		return projected, err
	}
	if state.SessionId != sid {
		return projected, errors.New("original worker state mismatch")
	}
	if state.Run.Active == nil && !workerNativeActive(state) {
		// active is optional in the native contract. A fresh terminal state
		// from this exact owner also confirms inactivity, but says nothing
		// about whether its transcript can be read successfully.
		switch value(state.Run.Status) {
		case "completed", "failed", "cancelled", "stopped", "interrupted":
		default:
			return projected, errors.New("original worker activity unconfirmed")
		}
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
		return s.workerViewLocked(current), errors.New("original worker or projection changed; read again")
	}
	created := p == nil
	if p == nil {
		p = &view{Items: []api.Item{}, Seen: map[string]bool{}}
		s.state.Views[sid] = p
	}
	prior := p.State
	priorHead := p.HeadTarget
	priorRetired := maps.Clone(p.RetiredTurns)
	if retiredRunHead(p, value(state.Run.TurnId)) {
		return projected, errors.New("original worker read referenced an older turn")
	}
	retireRunHead(p, value(state.Run.TurnId))
	observeStateHead(p, state.Run)
	p.State = state
	p.Observed++
	current.Activity = "idle"
	if workerNativeActive(state) {
		current.Activity = "active"
	}
	s.state.Workers[id] = current
	if err := s.saveLocked(); err != nil {
		if created {
			delete(s.state.Views, sid)
		} else {
			p.State = prior
			p.HeadTarget = priorHead
			p.RetiredTurns = priorRetired
			p.Observed--
		}
		s.state.Workers[id] = w
		return projected, err
	}
	s.bumpLocked()
	s.requestRefreshLocked("worker:" + id)
	return s.workerViewLocked(current), nil
}

func (s *Session) WorkMessageRecorded(in api.TaskMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.state.Workers[in.ID]
	op := w.operationID("send", in.RequestID)
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
	w.Activity = "" // A previous turn cannot prove the new dispatch idle.
	w.Task.Status, w.Task.Outcome, w.Task.Result = "unknown", "unknown", ""
	s.state.Workers[w.Task.ID] = w
	if err := s.saveLocked(); err != nil {
		s.state.Workers[w.Task.ID] = previous
		return previous, err
	}
	s.bumpLocked()
	s.requestRefreshLocked("worker:" + w.Task.ID)
	return w, nil
}

// nativeWorkerRefusal is only used for a fresh, exact-operation refusal after
// persistence succeeds. Native details remain an internal cause for the caller's
// diagnostic projection; the local code carries the refusal classification.
func nativeWorkerRefusal(result wire.CommandResult, cause error) error {
	code := "native_rejected"
	if result.Outcome == "conflicted" {
		code = "native_conflict"
	}
	if cause == nil {
		var details []string
		for _, detail := range []string{string(result.ErrorCode), string(result.ErrorKind), value(result.Detail)} {
			if detail != "" {
				details = append(details, detail)
			}
		}
		if len(details) == 0 {
			details = append(details, code)
		}
		cause = errors.New(strings.Join(details, ": "))
	}
	return &api.WorkMessageRejection{Cause: cause, Code: code}
}

func (s *Session) finishWorkerSubmission(w worker, result wire.CommandResult, callErr error) (api.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := w
	switch result.Outcome {
	case "rejected", "conflicted":
		w.Task = w.Submission.BeforeTask
		w.Task.Outcome = string(result.Outcome)
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
	// Only this newly submitted invocation reaches finishWorkerSubmission.
	// Preserve explicit pre-dispatch/native refusal, not an older projection or
	// a failed lookup of an unknown operation. Persistence must succeed first.
	if result.OperationId == previous.Submission.OperationID && (result.Outcome == "rejected" || result.Outcome == "conflicted") {
		callErr = nativeWorkerRefusal(result, callErr)
	}
	return s.workerViewLocked(w), callErr
}

func (s *Session) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	call, e := s.authority(ctx)
	if e != nil {
		return api.Task{}, e
	}
	inputs, e := readWorkerInputs(in.Files)
	if e != nil {
		return api.Task{}, e
	}
	digests := workerInputDigests(inputs)
	if len(in.FileDigests) != 0 && !slices.Equal(in.FileDigests, digests) {
		return api.Task{}, errors.New("Worker input digest differs")
	}
	in.FileDigests = digests
	ctx = withWorkIntent(ctx, in.ID, in.RequestID, "send", api.WorkFingerprint(in))
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	w, ok := s.state.Workers[in.ID]
	v := s.state.Views[w.Binding.SessionId]
	op := w.operationID("send", in.RequestID)
	_, retry := s.state.Operations[op]
	busy := v != nil && (value(v.State.Run.Active) || v.State.Approval.Active != nil)
	projected := s.workerViewLocked(w)
	unresolved := projected.Status == "unknown" || projected.Status == "pending" || w.Start != nil
	s.mu.Unlock()
	if !ok || w.Binding.SessionId == "" || v == nil {
		return api.Task{}, errors.New("任务未就绪")
	}
	if err := s.workerWriteAllowed(w); err != nil {
		return w.Task, err
	}
	sid := w.Binding.SessionId
	if retry && w.PromptID == op && w.Native {
		parts, encodeErr := s.encodeWorkerInputs(ctx, sid, op, inputs)
		if encodeErr != nil {
			return projected, encodeErr
		}
		res, err := s.submitNativeInput(ctx, sid, op, in.Prompt, parts, busy)
		projected.Outcome = productOutcome(res.Outcome)
		if res.Outcome == "conflicted" {
			projected.Outcome = "conflicted"
		}
		if res.Outcome == "unknown" && err == nil {
			err = errors.New("original request receipt remains unknown")
		}
		return projected, err
	}
	if unresolved {
		return projected, errors.New("original receipt unconfirmed; continuation refused")
	}
	if retry {
		return projected, errors.New("original request receipt already exists; read the exact request before continuing")
	}
	parts, e := s.encodeWorkerInputs(ctx, sid, op, inputs)
	if e != nil {
		return projected, e
	}
	if w.Native || s.isSteeringRetry(op) || busy && !retry {
		w, e = s.beginWorkerSubmission(w, op, busy)
		if e != nil {
			return projected, e
		}
		res, err := s.submitNativeInput(ctx, sid, op, in.Prompt, parts, busy)
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
	res, e := s.command(ctx, op, "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &op, SessionId: &sid, SourceKind: pointer("authorized_background"), GrantId: &g.Id, Input: &in.Prompt, ContentParts: parts})
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
	if err := s.workerWriteAllowed(w); err != nil {
		return w.Task, err
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
	return s.refreshSelectedWorkers(ctx, c, nil)
}

func (s *Session) refreshSelectedWorkers(ctx context.Context, c *client, selected observationPlan) error {
	s.mu.Lock()
	workers := clone(s.state.Workers)
	generation, instance := s.generation, s.state.InstanceID
	s.mu.Unlock()
	var failures error
	for id, w := range workers {
		if !selected.includes("worker:" + id) {
			continue
		}
		s.mu.Lock()
		needed := s.workerNeedsObservationLocked(w)
		s.mu.Unlock()
		if !needed {
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
			s.mu.Lock()
			if current, ok := s.state.Workers[id]; ok && s.client == c && s.generation == generation && sameWorkerOwner(w, current) {
				current.Activity = ""
				s.state.Workers[id] = current
			}
			s.mu.Unlock()
			failures = errors.Join(failures, e)
			continue
		}
		if v.SessionId != sid {
			failures = errors.Join(failures, errors.New("工作状态来源不匹配"))
			continue
		}
		s.mu.Lock()
		if s.client != c || s.generation != generation || s.state.InstanceID != instance || !sameWorkerOwner(w, s.state.Workers[id]) || s.closed {
			s.mu.Unlock()
			continue
		}
		p := s.state.Views[sid]
		if p == nil {
			p = &view{Items: []api.Item{}, Seen: map[string]bool{}}
			s.state.Views[sid] = p
		}
		if p == before && p.Observed == observed || before == nil && p.Observed == 0 {
			if retiredRunHead(p, value(v.Run.TurnId)) {
				s.mu.Unlock()
				failures = errors.Join(failures, errors.New("original worker read referenced an older turn"))
				continue
			}
			retireRunHead(p, value(v.Run.TurnId))
			approval := p.State.Approval
			p.State = v
			s.observeWorkerActivityLocked(sid, v)
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

// Legacy retired flags are ignored. Preserve identity and effect receipts; an
// unavailable cache is not a native execution result or a permission decision.
func (v *worker) UnmarshalJSON(raw []byte) error {
	type plain worker
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	if decoded.Task.Status == "unavailable" {
		decoded.Task.Status = "unknown"
	}
	if decoded.Submission != nil && decoded.Submission.BeforeTask.Status == "unavailable" {
		decoded.Submission.BeforeTask.Status = "unknown"
	}
	*v = worker(decoded)
	return nil
}

// Only fresh facts from the current authenticated client reach this helper.
func (s *Session) observeWorkerActivityLocked(sid string, state wire.SessionState) {
	for id, w := range s.state.Workers {
		if w.Binding.SessionId != sid {
			continue
		}
		w.Activity = ""
		if workerNativeActive(state) {
			w.Activity = "active"
		} else if state.Run.Active != nil {
			w.Activity = "idle"
		}
		s.state.Workers[id] = w
	}
}
