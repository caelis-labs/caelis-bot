package caelis

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/backend/contextseed"
)

func (s *Session) cleanupContextLocked() {
	if p := s.state.Context.Pending; p == nil || !p.Accepted {
		return
	}
	if s.saveLocked() != nil {
		return
	}
	if s.state.Context.Cleanup(s.tools) == nil {
		_ = s.saveLocked()
	}
}
func (s *Session) conversationLocked() api.ConversationState {
	out := api.ConversationState{Session: s.state.Session.SessionId}
	if s.tools != nil {
		out.DesiredRuntimeVersion = s.tools.RuntimeVersion
	}
	out.RuntimeVersion, _ = strings.CutPrefix(s.state.Session.Profile.Version, "caelis-bot-application-v1/")
	if v := s.state.Views[out.Session]; v != nil {
		out.Observed = s.connected && !s.closed && v.CommandCaughtUp
		out.Turn = observedTurn(v)
		if v.UsageTurn == out.Turn && v.ModelTurn == out.Turn {
			out.Usage = v.Usage
		}
		out.Status = v.Turns[out.Turn]
		if out.Status == "" {
			out.Status = value(v.State.Run.Status)
		}
		out.Idle = s.connected && !s.closed && !value(v.State.Run.Active) && value(v.State.Run.Status) != "unknown"
		for _, view := range s.state.Views {
			if view.State.Approval.Active != nil {
				out.Idle = false
			}
		}
		for _, operation := range s.state.Operations {
			if operation.Outcome == "unknown" {
				out.Idle = false
			}
		}
		for _, f := range s.state.CommandFollowups {
			if !f.Done {
				out.Idle = false
			}
		}
	}
	return out
}
func (s *Session) ConversationState() api.ConversationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conversationLocked()
}
func (s *Session) SubmitDream(ctx context.Context, in api.Submission) (api.Receipt, error) {
	in.Dream, in.Scheduled = true, true
	return s.submit(ctx, in, nil, "application_summary")
}
func (s *Session) DreamResult(id string) (api.Receipt, api.ConversationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.state.Operations[id]
	out := api.Receipt{ID: id, Outcome: "unknown"}
	result := api.ConversationState{Turn: j.TurnID}
	if !j.Dream {
		out.Outcome = "rejected"
		return out, result
	}
	out.Outcome = productOutcome(wire.Outcome(j.Outcome))
	for sid, v := range s.state.Views {
		if j.Path != "/application/sessions/"+idPath(sid)+"/prompt" {
			continue
		}
		result.Session, result.Status = sid, v.Turns[j.TurnID]
		if result.Status == "" && observedTurn(v) == j.TurnID {
			result.Status = value(v.State.Run.Status)
		}
		if result.Status == "completed" {
			hasRecap := false
			for _, i := range v.Items {
				if i.TurnKey == j.TurnID && i.Kind == "assistant" && strings.TrimSpace(i.Text) != "" {
					hasRecap = true
				}
			}
			if !hasRecap {
				result.Status = "failed"
			}
		}
	}
	return out, result
}
func (s *Session) CancelDream(ctx context.Context, id string) error {
	s.step.Lock()
	defer s.step.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	j := s.state.Operations[id]
	sid := s.state.Session.SessionId
	v := s.state.Views[sid]
	if j.Dream && v != nil && j.TurnID != "" && terminalDream(v.Turns[j.TurnID]) {
		s.mu.Unlock()
		return nil
	}
	if !j.Dream || v == nil || j.Path != "/application/sessions/"+idPath(sid)+"/prompt" || j.TurnID == "" || j.TurnID != value(v.State.Run.TurnId) {
		s.mu.Unlock()
		return errors.New("整理请求尚未确认，请恢复连接核对")
	}
	target := wire.TurnTarget{HandleId: value(v.State.Run.HandleId), RunId: value(v.State.Run.RunId), TurnId: j.TurnID}
	s.mu.Unlock()
	op := "dream-cancel-" + digest([]byte(id))
	out, err := s.command(ctx, op, "/sessions/"+idPath(sid)+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	if err != nil {
		return err
	}
	if !succeeded(out.Outcome) {
		return errors.New("整理停止结果尚未确认")
	}
	for {
		s.mu.Lock()
		v = s.state.Views[sid]
		done := v != nil && !value(v.State.Run.Active) && terminalDream(value(v.State.Run.Status))
		ch := s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func terminalDream(status string) bool {
	return status == "completed" || status == "failed" || status == "interrupted" || status == "cancelled" || status == "stopped"
}

func (s *Session) RenewConversation(ctx context.Context, id, source string) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if s.state.RenewedBy == id {
		s.mu.Unlock()
		return nil
	}
	current := s.conversationLocked()
	j := s.state.Operations[id]
	if !current.Observed || current.Session != source || !j.Dream || current.Turn != j.TurnID || current.Status != "completed" {
		s.mu.Unlock()
		return errors.New("对话已有新活动，暂不能交接")
	}
	op := "dream-renew-" + digest([]byte(id))
	pending, exists := s.state.Operations[op]
	c, life := s.client, s.state.Connection
	grants := clone(s.state.Grants)
	s.mu.Unlock()
	var renewalErr error
	if !exists {
		if !current.Idle {
			return errors.New("对话暂不能交接")
		}
		config, err := s.configuration(ctx, source)
		if err != nil {
			return err
		}
		profile := config.Profile
		if profile.Version != s.profile.Version {
			// New product assembly adopts current host environment defaults; CWD,
			// selected model and the independent sandbox scope remain preserved.
			profile.Version = s.profile.Version
			profile.ExecutionConfig = clone(s.profile.ExecutionConfig)
		}
		s.configureReviewer(&profile)
		profile.Instructions, profile.Tools, profile.ToolsVersion = s.profile.Instructions, s.profile.Tools, s.profile.ToolsVersion
		_, renewalErr = s.command(ctx, op, "/application/sessions", wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile})
	} else if pending.Outcome == "unknown" {
		renewalErr = s.recoverOperations(ctx)
	}
	s.mu.Lock()
	pending = s.state.Operations[op]
	if pending.Path == "/application/sessions" && (pending.Outcome == "rejected" || pending.Outcome == "conflicted") && s.state.Session.SessionId == source {
		// HTTP rejection may return both a known outcome and an error. Persist
		// the receipt before allowing the host to retire this renewal attempt.
		err := s.saveLocked()
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return api.ErrConversationRenewalRejected
	}
	s.mu.Unlock()
	if renewalErr != nil {
		return renewalErr
	}
	if !succeeded(wire.Outcome(pending.Outcome)) || pending.Resource == "" {
		return errors.New("新上下文创建结果尚未确认")
	}
	sid := pending.Resource
	var next wire.ApplicationBinding
	if err := c.json(ctx, "GET", "/application/sessions/"+idPath(sid), nil, &next, "", ""); err != nil {
		return err
	}
	if next.SessionId != sid || sid == source || next.ApplicationId != life.ApplicationId || next.ConnectionId != life.ConnectionId || next.PrincipalId != life.PrincipalId || next.Archived || next.Profile.Execution != s.executionMode {
		return errors.New("新上下文绑定不匹配")
	}
	if err := s.checkReviewer(ctx, next); err != nil {
		return err
	}
	var state wire.SessionState
	if err := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &state, "", ""); err != nil {
		return err
	}
	if state.SessionId != sid || value(state.Run.Active) {
		return errors.New("新上下文状态不匹配")
	}
	// Reattach the same existing authorization to the new internal session.
	for key, old := range grants {
		if old.Grant.Revoked {
			continue
		}
		g, err := s.createGrant(ctx, sid, old.Grant.Source, old.Grant.AuthorizationOperationId)
		if err != nil {
			return err
		}
		grants[key] = grantRecord{Grant: g, Fingerprint: old.Fingerprint}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current = s.conversationLocked()
	if !current.Observed || current.Session != source || current.Turn != j.TurnID || current.Status != "completed" || !current.Idle {
		return errors.New("原对话已有新活动，交接未切换")
	}
	old := s.state
	s.state.Session, s.state.RenewedBy, s.state.Grants = next, id, grants
	s.state.Context = contextseed.State{}
	s.state.PastSessions = append(append([]string{}, old.PastSessions...), source)
	s.state.Views[sid] = &view{State: state, Items: []api.Item{}, Seen: map[string]bool{}}
	if err := s.saveLocked(); err != nil {
		delete(s.state.Views, sid)
		s.state = old
		return err
	}
	s.ensureStreamLocked(sid)
	s.bumpLocked()
	return nil
}

var _ api.ConversationRuntime = (*Session)(nil)
