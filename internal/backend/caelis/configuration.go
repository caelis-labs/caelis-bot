package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// typedMutation journals exact requests before dispatch. A lost result is only
// reconciled through its documented receipt route, never resent under a new ID.
func (s *Session) typedMutation(ctx context.Context, op, path string, body any, out any) error {
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	s.mu.Lock()
	old, exists := s.state.Typed[op]
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return errors.New("Caelis 尚未连接")
	}
	if exists {
		hash := old.Digest
		if hash == "" {
			hash = digest(old.Body)
		}
		if old.Path != path || hash != digest(raw) {
			return errors.New("操作标识已用于其他请求")
		}
		if old.Outcome == "rejected" {
			return errors.New("原操作被拒绝，请重新核对配置")
		}
		if len(old.Result) > 0 {
			return json.Unmarshal(old.Result, out)
		}
		receiptID := op
		if old.ReceiptID != "" {
			receiptID = old.ReceiptID
		}
		var receipt wire.CommandResult
		if e = c.json(ctx, "GET", "/operations/"+idPath(receiptID), nil, &receipt, "", ""); e != nil {
			return e
		}
		if e := s.recordTypedRejection(op, receiptID, old, receipt); e != nil {
			return e
		}
		result, e := typedPayload(receiptID, path, receipt)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(result, out); e != nil {
			return e
		}
		old.Result = result
		old.Outcome = "committed"
	} else {
		old = typedRecord{Path: path, Body: raw, Digest: digest(raw), Outcome: "unknown"}
		s.mu.Lock()
		s.state.Typed[op] = old
		e = s.saveLocked()
		if e == nil && strings.HasSuffix(path, "/configuration") {
			s.requestRefreshLocked("configuration")
		}
		s.mu.Unlock()
		if e != nil {
			return e
		}
		var receipt wire.CommandResult
		e = c.json(ctx, "POST", path, body, &receipt, op, "")
		if e != nil {
			var remote *remoteError
			if errors.As(e, &remote) && ((remote.Status >= 400 && remote.Status < 500) || remote.Code == "unsupported" || remote.Code == "invalid_argument") {
				old.Outcome = "rejected"
				s.mu.Lock()
				s.state.Typed[op] = old
				save := s.saveLocked()
				s.mu.Unlock()
				return errors.Join(e, save)
			}
			return e
		}
		if e := s.recordTypedRejection(op, op, old, receipt); e != nil {
			return e
		}
		result, e := typedPayload(op, path, receipt)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(result, out); e != nil {
			return e
		}
		old.Result = result
		old.Outcome = "committed"
	}
	// Confirmed uploads must not keep base64 file bytes in the binding forever.
	// The digest still rejects changed retries; unknown operations retain intent.
	old.Digest = digest(raw)
	old.Body = nil
	s.mu.Lock()
	s.state.Typed[op] = old
	// A committed receipt and the old configuration must never be persisted
	// together as if the old profile were current. A subsequent public GET
	// establishes the actual configuration before sending is admitted.
	if strings.HasSuffix(path, "/configuration") {
		delete(s.state.Configurations, s.state.Session.SessionId)
	}
	e = s.saveLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) configuration(ctx context.Context, sid string) (wire.SessionConfiguration, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	var v wire.SessionConfiguration
	if c == nil {
		return v, errors.New("Caelis 尚未连接")
	}
	e := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/configuration", nil, &v, "", "")
	if e != nil {
		return v, e
	}
	return v, s.acceptConfiguration(sid, v)
}
func (s *Session) acceptConfiguration(sid string, v wire.SessionConfiguration) error {
	if v.SessionId != sid || v.Revision == "" {
		return errors.New("Caelis 配置回执不匹配")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.state.Configurations[sid]; old.Profile.Model != v.Profile.Model {
		if view := s.state.Views[sid]; view != nil {
			view.Usage, view.UsageTurn, view.ModelTurn = api.ContextUsage{}, "", ""
		}
	}
	s.state.Configurations[sid] = v
	return s.saveLocked()
}

// Configuration exposes desired vs actually used revisions to host acceptance.
func (s *Session) Configuration(ctx context.Context) (wire.SessionConfiguration, error) {
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	return s.configuration(ctx, sid)
}
func (s *Session) UpdateConfiguration(ctx context.Context, op, revision string, patch map[string]any) (wire.SessionConfiguration, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	return s.updateConfiguration(ctx, sid, op, revision, patch)
}
func (s *Session) updateConfiguration(ctx context.Context, sid, op, revision string, patch map[string]any) (wire.SessionConfiguration, error) {
	if _, changesModel := patch["model"]; changesModel {
		s.mu.Lock()
		if view := s.state.Views[sid]; view != nil {
			view.Usage, view.UsageTurn, view.ModelTurn = api.ContextUsage{}, "", ""
		}
		s.mu.Unlock()
	}
	// A map preserves explicit [] vs omitted catalogs; generated omitempty slices
	// cannot represent a catalog clear. Do not alter the pinned generated types.
	body := map[string]any{"operation_id": op, "expected_configuration_revision": revision, "patch": patch}
	var v wire.SessionConfiguration
	e := s.typedMutation(ctx, op, "/sessions/"+idPath(sid)+"/configuration", body, &v)
	if e != nil {
		return v, e
	}
	if v.SessionId != sid || v.Revision == "" {
		return v, errors.New("Caelis 配置更新回执不匹配")
	}
	// Operation receipts are immutable historical values. A replay must not roll
	// the cached desired configuration backward after later committed updates.
	_, e = s.configuration(ctx, sid)
	return v, e
}
func (s *Session) recoverConfigurations(ctx context.Context) error {
	hadPluginRecovery := s.BotPluginRecoveryPending()
	s.mu.Lock()
	records := clone(s.state.Typed)
	s.mu.Unlock()
	for op, r := range records {
		if !strings.HasSuffix(r.Path, "/configuration") {
			continue
		}
		if r.Outcome == "committed" {
			s.mu.Lock()
			sid := s.state.Session.SessionId
			missing := r.Path == "/sessions/"+idPath(sid)+"/configuration" && s.state.Configurations[sid].Revision == ""
			s.mu.Unlock()
			if missing {
				if _, e := s.configuration(ctx, sid); e != nil {
					return e
				}
			}
			continue
		}
		if r.Outcome != "unknown" {
			continue
		}
		var v wire.SessionConfiguration
		if e := s.typedMutation(ctx, op, r.Path, json.RawMessage(r.Body), &v); e != nil {
			return e
		}
		s.mu.Lock()
		sid := s.state.Session.SessionId
		s.mu.Unlock()
		if v.SessionId != sid || v.Revision == "" {
			return errors.New("Caelis 配置恢复回执不匹配")
		}
		if _, e := s.configuration(ctx, v.SessionId); e != nil {
			return e
		}
	}
	if hadPluginRecovery && !s.BotPluginRecoveryPending() {
		// The connection was already ready before this original receipt was
		// resolved. Wake the host observer to project its confirmed selection.
		s.mu.Lock()
		s.bumpLocked()
		s.mu.Unlock()
	}
	return nil
}

// Called with s.mu. Receipt recovery and subsequent configuration readback are
// both required even while the Bot is idle and its event stream is healthy.
func (s *Session) configurationRecoveryPendingLocked() bool {
	sid := s.state.Session.SessionId
	for _, record := range s.state.Typed {
		if !strings.HasSuffix(record.Path, "/configuration") {
			continue
		}
		if record.Outcome == "unknown" || record.Outcome == "committed" && record.Path == "/sessions/"+idPath(sid)+"/configuration" && s.state.Configurations[sid].Revision == "" {
			return true
		}
	}
	return false
}

func (s *Session) maintainConfigurations(ctx context.Context) error {
	s.mu.Lock()
	due := s.configurationRecoveryPendingLocked() && !time.Now().Before(s.configurationRetryAt)
	s.mu.Unlock()
	if !due {
		return nil
	}
	err := s.recoverConfigurations(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configurationRecoveryPendingLocked() {
		s.configurationRetryAt, s.configurationRetryDelay = time.Time{}, 0
	} else {
		if s.configurationRetryDelay == 0 {
			s.configurationRetryDelay = 2 * time.Second
		} else {
			s.configurationRetryDelay = min(30*time.Second, 2*s.configurationRetryDelay)
		}
		s.configurationRetryAt = time.Now().Add(s.configurationRetryDelay)
	}
	return err
}

// typedPayload decodes only the original committed common command receipt.
// Current configuration or resource lists cannot prove a past mutation.
func typedPayload(operation, path string, receipt wire.CommandResult) (json.RawMessage, error) {
	if receipt.OperationId != operation || receipt.Outcome != wire.OutcomeCommitted {
		return nil, errors.New("Caelis 原操作尚未确认提交")
	}
	var payload any
	switch {
	case strings.HasSuffix(path, "/configuration"):
		if receipt.Configuration == nil {
			return nil, errors.New("Caelis 配置回执缺失")
		}
		payload = receipt.Configuration
	case strings.HasSuffix(path, "/resources"):
		if receipt.SessionResource == nil {
			return nil, errors.New("Caelis 资源回执缺失")
		}
		payload = receipt.SessionResource
	case strings.Contains(path, "/background-grants"):
		if receipt.BackgroundGrant == nil {
			return nil, errors.New("Caelis 后台授权回执缺失")
		}
		payload = receipt.BackgroundGrant
	default:
		return nil, errors.New("未知的 Caelis 操作回执类型")
	}
	return json.Marshal(payload)
}

func (s *Session) recordTypedRejection(op, receiptID string, record typedRecord, receipt wire.CommandResult) error {
	if receipt.OperationId != receiptID || receipt.Outcome != wire.OutcomeRejected && receipt.Outcome != wire.OutcomeConflicted {
		return nil
	}
	record.Outcome = "rejected"
	s.mu.Lock()
	s.state.Typed[op] = record
	err := s.saveLocked()
	s.mu.Unlock()
	return errors.Join(errors.New("Caelis 已拒绝原操作，请核对配置"), err)
}
