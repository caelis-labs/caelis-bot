package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// typedMutation journals exact requests before dispatch. A lost result is only
// reconciled through its documented receipt route, never resent under a new ID.
func (s *Session) typedMutation(ctx context.Context, op, path, recovery string, body any, out any) error {
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	s.mu.Lock()
	old, exists := s.state.Typed[op]
	c := s.client
	s.mu.Unlock()
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
		if recovery == "" {
			return errors.New("操作结果未确认，不能重复执行")
		}
		var result json.RawMessage
		if e = c.json(ctx, "GET", recovery, nil, &result, "", ""); e != nil {
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
		s.mu.Unlock()
		if e != nil {
			return e
		}
		var result json.RawMessage
		e = c.json(ctx, "POST", path, body, &result, op, "")
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
	e = s.saveLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) configuration(ctx context.Context, sid string) (wire.ApplicationConfiguration, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	var v wire.ApplicationConfiguration
	if c == nil {
		return v, errors.New("Caelis 尚未连接")
	}
	e := c.json(ctx, "GET", "/application/sessions/"+idPath(sid)+"/configuration", nil, &v, "", "")
	if e != nil {
		return v, e
	}
	return v, s.acceptConfiguration(sid, v)
}
func (s *Session) acceptConfiguration(sid string, v wire.ApplicationConfiguration) error {
	if v.SessionId != sid || v.Revision == "" {
		return errors.New("Caelis 配置回执不匹配")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Configurations[sid] = v
	return s.saveLocked()
}

// Configuration exposes desired vs actually used revisions to host acceptance.
func (s *Session) Configuration(ctx context.Context) (wire.ApplicationConfiguration, error) {
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	return s.configuration(ctx, sid)
}
func (s *Session) UpdateConfiguration(ctx context.Context, op, revision string, patch map[string]any) (wire.ApplicationConfiguration, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	return s.updateConfiguration(ctx, sid, op, revision, patch)
}
func (s *Session) updateConfiguration(ctx context.Context, sid, op, revision string, patch map[string]any) (wire.ApplicationConfiguration, error) {
	// A map preserves explicit [] vs omitted catalogs; generated omitempty slices
	// cannot represent a catalog clear. Do not alter the pinned generated types.
	body := map[string]any{"operation_id": op, "expected_configuration_revision": revision, "patch": patch}
	var v wire.ApplicationConfiguration
	e := s.typedMutation(ctx, op, "/application/sessions/"+idPath(sid)+"/configuration", "/application/configuration-operations/"+idPath(op), body, &v)
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
	s.mu.Lock()
	records := clone(s.state.Typed)
	s.mu.Unlock()
	for op, r := range records {
		if r.Outcome != "unknown" || !strings.HasSuffix(r.Path, "/configuration") {
			continue
		}
		var v wire.ApplicationConfiguration
		if e := s.typedMutation(ctx, op, r.Path, "/application/configuration-operations/"+idPath(op), json.RawMessage(r.Body), &v); e != nil {
			return e
		}
	}
	return nil
}
