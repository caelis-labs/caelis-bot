package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

type grantRecord struct {
	Grant       wire.ApplicationBackgroundGrant `json:"grant"`
	Fingerprint string                          `json:"fingerprint"`
}

func (s *Session) createGrant(ctx context.Context, sid, source, authorization string) (wire.ApplicationBackgroundGrant, error) {
	op := "grant-" + digest([]byte(sid+"\x00"+source+"\x00"+authorization))
	path := "/application/sessions/" + idPath(sid) + "/background-grants"
	s.mu.Lock()
	c := s.client
	pending, exists := s.state.Typed[op]
	s.mu.Unlock()
	if exists && pending.Outcome == "unknown" {
		// List/read recovery uses the stored request's source and authorization,
		// never re-enrolls a grant after an uncertain reply.
		var grants []wire.ApplicationBackgroundGrant
		if e := c.json(ctx, "GET", path, nil, &grants, "", ""); e != nil {
			return wire.ApplicationBackgroundGrant{}, e
		}
		for _, g := range grants {
			if g.Source == source && g.AuthorizationOperationId == authorization && !g.Revoked {
				return g, nil
			}
		}
	}
	var g wire.ApplicationBackgroundGrant
	e := s.typedMutation(ctx, op, path, "", wire.ApplicationBackgroundGrantRequest{OperationId: op, Source: source, AuthorizationOperationId: authorization}, &g)
	if e == nil && (g.SessionId != sid || g.Source != source || g.Revoked) {
		e = errors.New("后台授权不匹配或已撤销")
	}
	return g, e
}
func (s *Session) AuthorizeBackground(ctx context.Context, id, fingerprint string) error {
	call, e := s.authority(ctx)
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
func (s *Session) revokeGrant(ctx context.Context, g wire.ApplicationBackgroundGrant) error {
	var out wire.ApplicationBackgroundGrant
	e := s.client.json(ctx, "POST", "/application/sessions/"+idPath(g.SessionId)+"/background-grants/"+idPath(g.Id)+"/revoke", struct{}{}, &out, "", "")
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
func (s *Session) SubmitBackground(ctx context.Context, in api.Submission, ids []string) (api.Receipt, error) {
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
