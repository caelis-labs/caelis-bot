package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type grantRecord struct {
	Grant       wire.SessionBackgroundGrant `json:"grant"`
	Fingerprint string                      `json:"fingerprint"`
}

func (s *Session) createGrant(ctx context.Context, sid, source, authorization string) (wire.SessionBackgroundGrant, error) {
	op := "grant-" + digest([]byte(sid+"\x00"+source+"\x00"+authorization))
	path := "/sessions/" + idPath(sid) + "/background-grants"
	var g wire.SessionBackgroundGrant
	e := s.typedMutation(ctx, op, path, wire.CreateSessionBackgroundGrantRequest{OperationId: op, Source: source, AuthorizationOperationId: authorization}, &g)
	if e == nil && (g.SessionId != sid || g.Source != source || g.Revoked) {
		e = errors.New("后台授权不匹配或已撤销")
	}
	return g, e
}
func (s *Session) AuthorizeBackground(ctx context.Context, id, fingerprint string) error {
	call, e := s.currentInvocation(ctx)
	if e != nil {
		return e
	}
	if call.Source.Kind != "user" {
		return errors.New("新提醒需要用户请求")
	}
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	old, ok := s.state.Grants[id]
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	if ok && old.Fingerprint == fingerprint && !old.Grant.Revoked {
		return nil
	}
	g, e := s.createGrant(ctx, sid, "schedule-"+digest([]byte(id+fingerprint)), call.Source.OperationId)
	if e != nil {
		return e
	}
	if ok && !old.Grant.Revoked {
		if e = s.revokeGrant(ctx, old.Grant); e != nil {
			return e
		}
	}
	s.mu.Lock()
	s.state.Grants[id] = grantRecord{g, fingerprint}
	e = s.saveLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) revokeGrant(ctx context.Context, g wire.SessionBackgroundGrant) error {
	var out wire.SessionBackgroundGrant
	op := "revoke-" + g.Id
	e := s.typedMutation(ctx, op, "/sessions/"+idPath(g.SessionId)+"/background-grants/"+idPath(g.Id)+"/revoke", wire.RevokeSessionBackgroundGrantRequest{OperationId: op, SessionId: &g.SessionId, GrantId: &g.Id}, &out)
	if e == nil && (out.Id != g.Id || !out.Revoked) {
		return errors.New("撤销后台授权未确认")
	}
	return e
}
func (s *Session) RevokeBackground(ctx context.Context, id string) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	r, ok := s.state.Grants[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	if e := s.revokeGrant(ctx, r.Grant); e != nil {
		return e
	}
	r.Grant.Revoked = true
	s.mu.Lock()
	s.state.Grants[id] = r
	e := s.saveLocked()
	s.mu.Unlock()
	return e
}
func (s *Session) submitScheduledSystem(ctx context.Context, in api.Submission, ids []string) (api.Receipt, error) {
	if len(ids) != 1 {
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, errors.New("后台激活必须对应一个已授权提醒")
	}
	s.mu.Lock()
	g, ok := s.state.Grants[ids[0]]
	s.mu.Unlock()
	if !ok || g.Grant.Revoked {
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, errors.New("提醒授权不可用")
	}
	return s.submitGrant(ctx, in, nil, "authorized_background", g.Grant.Id)
}

func (s *Session) SystemReceipt(id string) api.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.state.Operations[id]
	if !ok || (!j.Scheduled && !j.hostNotice()) {
		return api.Receipt{ID: id, Outcome: "unknown"}
	}
	return api.Receipt{ID: id, Outcome: productOutcome(wire.Outcome(j.Outcome))}
}
