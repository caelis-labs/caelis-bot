package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
		out.TurnKey = out.Turn
		if v.UsageTurn == out.Turn && v.ModelTurn == out.Turn && v.Usage.Window > 0 && !v.Usage.ModelAt.IsZero() {
			out.Usage = v.Usage
			out.UsageEvidence = "live_gauge_and_provider"
		} else if v.UsageEvidence != "" {
			out.UsageEvidence = v.UsageEvidence
		} else {
			out.UsageEvidence = "no_live_usage_pair"
		}
		out.Status = v.Turns[out.Turn]
		if out.Status == "" {
			out.Status = value(v.State.Run.Status)
		}
		out.Idle = s.connected && !s.closed && !value(v.State.Run.Active) && value(v.State.Run.Status) != "unknown"
		for sid, view := range s.state.Views {
			if view.State.Approval.Active != nil && !slices.Contains(s.state.PastSessions, sid) {
				out.Idle = false
			}
		}
		for id, operation := range s.state.Operations {
			if operation.Outcome == "unknown" && operation.Path != workspaceTrustPath && !operation.deferredRenewal(id) && !s.pastResidentOperationLocked(operation.Path) {
				out.Idle = false
			}
		}
		for _, f := range s.state.CommandFollowups {
			if !f.Done && f.belongsTo(out.Session) {
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
	in.HostNotice = true
	return s.submit(ctx, in, nil, "external_material")
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
		if j.Path != "/sessions/"+idPath(sid)+"/prompt" {
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
	if !j.Dream || v == nil || j.Path != "/sessions/"+idPath(sid)+"/prompt" || j.TurnID == "" || j.TurnID != value(v.State.Run.TurnId) {
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
	return s.renewConversation(ctx, id, source, "", nil)
}

func (s *Session) RenewIdleConversation(ctx context.Context, id string, head api.ConversationState) error {
	return s.renewConversation(ctx, id, head.Session, "", &head.Turn)
}

func (s *Session) RenewAfterTool(ctx context.Context, invocation api.ToolInvocation) (string, error) {
	if invocation.Provider != "caelis" || invocation.CallID == "" || invocation.Session == "" || invocation.Turn == "" {
		return "", errors.New("original application callback is unavailable")
	}
	if err := s.renewConversation(ctx, invocation.CallID, invocation.Session, invocation.Turn, nil); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Operations[toolRenewalID(invocation.CallID)].Resource, nil
}

func toolRenewalID(id string) string { return "tool-renew-" + digest([]byte(id)) }

func (s *Session) renewConversation(ctx context.Context, id, source, toolTurn string, idleTurn *string) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	if s.state.RenewedBy == id {
		s.mu.Unlock()
		return nil
	}
	op := "dream-renew-" + digest([]byte(id))
	if idleTurn != nil {
		op = "idle-renew-" + digest([]byte(id))
	} else if toolTurn != "" {
		op = toolRenewalID(id)
	}
	if pending, exists := s.state.Operations[op]; toolTurn != "" && exists {
		if !pending.RenewalDeferred && succeeded(wire.Outcome(pending.Outcome)) && slices.Contains(s.state.PastSessions, pending.Resource) {
			s.mu.Unlock()
			return nil // An earlier successful handoff has its own binding receipt.
		}
		// Original create receipts precede current-Turn admission. Recovery
		// reads the same operation; it cannot dispatch another create.
		if pending.Outcome == "unknown" {
			s.mu.Unlock()
			if err := s.recoverOperation(ctx, op); err != nil {
				return err
			}
			s.mu.Lock()
			pending = s.state.Operations[op]
		}
		if pending.RenewalDeferred || pending.Outcome == "unknown" || s.state.Session.SessionId != source {
			s.mu.Unlock()
			return s.recordDeferredRenewal(op, api.ErrConversationRenewalDeferred)
		}
	}
	current := s.conversationLocked()
	j := s.state.Operations[id]
	pending, exists := s.state.Operations[op]
	boundTurn := j.TurnID
	if idleTurn != nil {
		boundTurn = *idleTurn
	}
	matchesBoundary := func(current api.ConversationState) bool {
		return current.Observed && current.Turn == boundTurn && (idleTurn != nil || current.Status == "completed")
	}
	if current.Session != source {
		s.mu.Unlock()
		return api.ErrConversationRenewalRejected
	}
	if toolTurn != "" && current.Turn != "" && current.Turn != toolTurn {
		s.mu.Unlock()
		if exists {
			return s.recordDeferredRenewal(op, api.ErrConversationRenewalDeferred)
		}
		return api.ErrConversationRenewalRejected
	}
	if toolTurn == "" && (idleTurn == nil && !j.Dream || !matchesBoundary(current)) {
		s.mu.Unlock()
		return errors.New("对话已有新活动，暂不能交接")
	}
	for original, operation := range s.state.Operations {
		if original != op && operation.deferredRenewal(original) && operation.Outcome == "unknown" {
			s.mu.Unlock()
			return errors.Join(api.ErrConversationRenewalRejected, errors.New("original context creation is unresolved; retain its receipt"))
		}
	}
	c, principal := s.client, s.state.PrincipalID
	grants := clone(s.state.Grants)
	ready := s.connected && !s.closed && c != nil && s.renewalReadyLocked(source, op, toolTurn != "")
	s.mu.Unlock()
	if err := s.checkRenewalGuard(ctx, op); err != nil {
		return err
	}
	if !ready {
		return errors.New("对话暂不能交接")
	}
	if toolTurn != "" {
		if err := s.confirmHandoff(ctx, c, id, source, toolTurn); err != nil {
			return err
		}
	}
	var renewalErr error
	if !exists {
		config, err := s.configuration(ctx, source)
		if err != nil {
			return err
		}
		profile := *config.Profile
		profile.McpServers = withoutPrivateMCP(profile.McpServers)
		if profile.Version != s.profile.Version {
			// New product assembly adopts current host environment defaults; CWD,
			// selected model and the independent sandbox scope remain preserved.
			profile.Version = s.profile.Version
			profile.ExecutionConfig = clone(s.profile.ExecutionConfig)
		}
		s.configureApproval(&profile)
		profile.Instructions, profile.Tools, profile.ToolsVersion = s.profile.Instructions, s.profile.Tools, s.profile.ToolsVersion
		_, renewalErr = s.command(ctx, op, "/sessions", wire.CreateSessionRequest{OperationId: &op, Profile: &profile})
	} else if pending.Outcome == "unknown" {
		renewalErr = s.recoverOperation(ctx, op)
	}
	s.mu.Lock()
	pending = s.state.Operations[op]
	if pending.Path == "/sessions" && (pending.Outcome == "rejected" || pending.Outcome == "conflicted") && s.state.Session.SessionId == source {
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
	if (toolTurn != "" || idleTurn != nil) && pending.Path == "/sessions" && pending.Outcome == "unknown" {
		if err := s.deferRenewal(op); err != nil {
			return err
		}
		return api.ErrConversationRenewalUnknown
	}
	if renewalErr != nil {
		return renewalErr
	}
	if !succeeded(wire.Outcome(pending.Outcome)) || pending.Resource == "" {
		return errors.New("新上下文创建结果尚未确认")
	}
	sid := pending.Resource
	var next wire.SessionBinding
	if err := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/profile", nil, &next, "", ""); err != nil {
		return err
	}
	if next.SessionId != sid || sid == source || next.PrincipalId != principal || next.Archived || next.Profile.Execution != s.executionMode {
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
	if s.tools != nil && s.tools.CallResidentMCP != nil {
		config, err := s.configuration(ctx, sid)
		if err != nil {
			return err
		}
		servers := withoutPrivateMCP(config.Profile.McpServers)
		profile := s.profile
		if config.Profile.ToolsVersion != profile.ToolsVersion || len(servers) != len(config.Profile.McpServers) {
			patch := map[string]any{"mcp_servers": servers, "tools_version": profile.ToolsVersion, "tools": profile.Tools}
			intent, _ := json.Marshal(patch)
			change := "renew-private-" + digest([]byte(sid+"\x00"+string(config.Revision)+"\x00"+string(intent)))
			if _, err := s.updateConfiguration(ctx, sid, change, string(config.Revision), patch); err != nil {
				return err
			}
		}
	}
	if err := s.checkRenewalGuard(ctx, op); err != nil {
		return err
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
	if err := s.checkRenewalGuard(ctx, op); err != nil {
		return err
	}
	s.mu.Lock()
	current = s.conversationLocked()
	if toolTurn != "" && current.Turn != "" && current.Turn != toolTurn {
		s.mu.Unlock()
		return s.recordDeferredRenewal(op, api.ErrConversationRenewalDeferred)
	}
	if current.Session != source || !s.connected || s.closed || s.client != c || !s.renewalReadyLocked(source, op, toolTurn != "") ||
		toolTurn == "" && !matchesBoundary(current) {
		s.mu.Unlock()
		return errors.New("原对话已有新活动，交接未切换")
	}
	err := api.CommitRenewal(ctx, sid, func() error {
		old := s.state
		s.state.Session, s.state.RenewedBy, s.state.Grants = next, id, grants
		s.state.Context = contextseed.State{}
		s.state.CommandFollowups = clone(old.CommandFollowups)
		for id, followup := range s.state.CommandFollowups {
			if followup.Session == "" {
				followup.Session = source
				s.state.CommandFollowups[id] = followup
			}
		}
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
	})
	s.mu.Unlock()
	return s.recordDeferredRenewal(op, err)
}

func (s *Session) checkRenewalGuard(ctx context.Context, op string) error {
	return s.recordDeferredRenewal(op, api.CheckRenewalGuard(ctx))
}

func (s *Session) pastResidentOperationLocked(path string) bool {
	for _, sid := range s.state.PastSessions {
		if strings.HasPrefix(path, "/sessions/"+idPath(sid)+"/") {
			return true
		}
	}
	return false
}

func (s *Session) recordDeferredRenewal(op string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.state.Operations[op]
	if j.Path != "/sessions" || !j.RenewalDeferred && !errors.Is(err, api.ErrConversationRenewalDeferred) {
		return err
	}
	j.RenewalDeferred = true
	s.state.Operations[op] = j
	if saveErr := s.saveLocked(); saveErr != nil {
		return saveErr
	}
	s.bumpLocked()
	if j.Outcome == "unknown" {
		return api.ErrConversationRenewalUnknown
	}
	if j.Outcome == "rejected" || j.Outcome == "conflicted" {
		return api.ErrConversationRenewalRejected
	}
	return api.ErrConversationRenewalDeferred
}

func (s *Session) deferRenewal(op string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.state.Operations[op]
	j.RenewalDeferred = true
	s.state.Operations[op] = j
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.bumpLocked()
	return nil
}

// Renewal changes only the resident binding. Durable Worker requests and
// reports keep their owners; their activity is not a global-quiescence gate.
func (s *Session) renewalReadyLocked(source, renewal string, toolHandoff bool) bool {
	v := s.state.Views[source]
	if v == nil || !toolHandoff && (value(v.State.Run.Active) || v.State.Approval.Active != nil) {
		return false
	}
	for id, operation := range s.state.Operations {
		if operation.Outcome == "unknown" && id != renewal && !s.workerOperationLocked(operation) && !s.pastResidentOperationLocked(operation.Path) {
			return false
		}
	}
	// Native commands yielded by the resident still require their original
	// completion observation before its terminal handoff can be committed.
	if !toolHandoff {
		for _, followup := range s.state.CommandFollowups {
			if !followup.Done && followup.belongsTo(source) {
				return false
			}
		}
	}
	return true
}

var _ api.ConversationRuntime = (*Session)(nil)
