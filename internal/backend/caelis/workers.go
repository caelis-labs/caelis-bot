package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
type worker struct {
	Binding     wire.ApplicationBinding `json:"binding"`
	Task        api.Task                `json:"task"`
	Fingerprint string                  `json:"fingerprint"`
	PromptID    string                  `json:"promptID"`
	Stopped     bool                    `json:"stopped"`
	Start       *workerStart            `json:"start,omitempty"`
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
		if p := s.state.Views[w.Binding.SessionId]; p != nil {
			key = observedTurn(p)
		}
		out = append(out, api.WorkState{Task: v, ExecutionKey: key, StopRequested: w.Stopped, StartFingerprint: w.Fingerprint})
	}
	return out
}
func (s *Session) workerViewLocked(w worker) api.Task {
	out := w.Task
	if p := s.state.Views[w.Binding.SessionId]; p != nil {
		status := value(p.State.Run.Status)
		if value(p.State.Run.Active) {
			out.Status = "working"
		} else if status != "" {
			out.Status = status
		}
		if status == "cancelled" || status == "stopped" {
			out.Status = "interrupted"
		}
		if p.State.Approval.Active != nil {
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
	s.mu.Lock()
	old, exists := s.state.Workers[in.ID]
	cfg := s.state.Configurations[s.state.Session.SessionId]
	s.mu.Unlock()
	if exists {
		if old.Fingerprint != fp {
			return api.Task{}, errors.New("任务请求冲突")
		}
		return s.ReadWork(ctx, in.ID)
	}
	profile := cfg.Profile
	execution, e := s.resolveWorkExecution(ctx, profile)
	if e != nil {
		return api.Task{}, e
	}
	profile.Model = execution.Model
	profile.ReasoningEffort = pointer(execution.Effort)
	profile.ServiceTier = pointer(execution.ServiceTier)
	profile.Instructions = in.Instructions
	profile.Tools = nil
	profile.ToolsVersion = "worker-native-v1"
	profile.Workspace = &wire.ApplicationWorkspace{Cwd: &in.Workspace}
	w := worker{Task: api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "unknown", Outcome: "unknown"}, Fingerprint: fp, PromptID: "work-prompt-" + digest([]byte(in.RequestID)), Start: &workerStart{Profile: profile, Prompt: in.Prompt, Authorization: call.Source.OperationId}}
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
		result, e := s.command(ctx, op, "/application/sessions", wire.CreateApplicationSessionRequest{OperationId: &op, Profile: w.Start.Profile})
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
		if e = c.json(ctx, "GET", "/application/sessions/"+idPath(sid), nil, &w.Binding, "", ""); e != nil {
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
	grant, e := s.createGrant(ctx, sid, "work-"+w.Task.ID, w.Start.Authorization)
	if e != nil {
		return w.Task, e
	}
	result, e := s.command(ctx, w.PromptID, "/application/sessions/"+idPath(sid)+"/prompt", wire.ApplicationPromptRequest{OperationId: &w.PromptID, SessionId: &sid, SourceKind: "authorized_background", GrantId: &grant.Id, Input: &w.Start.Prompt})
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
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.state.Workers[id]
	if !ok {
		return api.Task{}, errors.New("任务不属于当前 Bot")
	}
	return s.workerViewLocked(w), nil
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
	busy := v != nil && (value(v.State.Run.Active) || v.State.Approval.Active != nil)
	s.mu.Unlock()
	if !ok || busy || w.Binding.SessionId == "" {
		return api.Task{}, errors.New("任务忙碌或未就绪，当前 Caelis 不支持在途追加")
	}
	sid := w.Binding.SessionId
	g, e := s.createGrant(ctx, sid, "continue-"+digest([]byte(in.RequestID)), call.Source.OperationId)
	if e != nil {
		return w.Task, e
	}
	op := "work-send-" + digest([]byte(in.RequestID))
	res, e := s.command(ctx, op, "/application/sessions/"+idPath(sid)+"/prompt", wire.ApplicationPromptRequest{OperationId: &op, SessionId: &sid, SourceKind: "authorized_background", GrantId: &g.Id, Input: &in.Prompt})
	s.mu.Lock()
	w.Stopped = false
	w.Task.Outcome = productOutcome(res.Outcome)
	w.PromptID = op
	s.state.Workers[in.ID] = w
	save := s.saveLocked()
	s.mu.Unlock()
	return w.Task, errors.Join(e, save)
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
	for id, w := range workers {
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
		var observed uint64
		if before != nil {
			observed = before.Observed
		}
		s.mu.Unlock()
		var v wire.SessionState
		if e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &v, "", ""); e != nil {
			return e
		}
		if v.SessionId != sid {
			return errors.New("工作状态来源不匹配")
		}
		s.mu.Lock()
		p := s.state.Views[sid]
		if p == nil {
			p = &view{Items: []api.Item{}, Seen: map[string]bool{}}
			s.state.Views[sid] = p
		}
		if p == before && p.Observed == observed || before == nil && p.Observed == 0 {
			p.State = v
		}
		s.ensureStreamLocked(sid)
		s.bumpLocked()
		e := s.saveLocked()
		s.mu.Unlock()
		if e != nil {
			return e
		}
	}
	return nil
}
