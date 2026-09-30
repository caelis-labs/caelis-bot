package caelis

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// InterruptTurn serializes the exact target check, authority revocation and
// cancel. Native Control validates the full target again at command admission.
func (s *Session) InterruptTurn(ctx context.Context, expected string, before func()) error {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid, instance, c := s.state.Session.SessionId, s.state.InstanceID, s.client
	v := s.state.Views[sid]
	if expected == "" || !s.connected || s.closed || c == nil || v == nil || !value(v.State.Run.Active) || observedTurn(v) != expected {
		s.mu.Unlock()
		return errors.New("observed turn is no longer active")
	}
	target := wire.TurnTarget{HandleId: value(v.State.Run.HandleId), RunId: value(v.State.Run.RunId), TurnId: value(v.State.Run.TurnId)}
	s.mu.Unlock()
	var head wire.SessionState
	if err := c.json(ctx, "GET", "/sessions/"+idPath(sid)+"/state", nil, &head, "", ""); err != nil {
		return err
	}
	current := wire.TurnTarget{HandleId: value(head.Run.HandleId), RunId: value(head.Run.RunId), TurnId: value(head.Run.TurnId)}
	if !value(head.Run.Active) || current != target || current.TurnId != expected {
		return errors.New("observed native turn changed")
	}
	if before != nil {
		before()
	}
	b, _ := json.Marshal(target)
	op := "cancel-" + digest(append([]byte(instance+"\x00"+sid+"\x00"), b...))
	out, err := s.command(ctx, op, "/sessions/"+idPath(sid)+"/cancel", wire.CancelRequest{OperationId: &op, SessionId: &sid, Target: target})
	if err != nil {
		return err
	}
	if !succeeded(out.Outcome) {
		return errors.New("exact interruption outcome unconfirmed")
	}
	return nil
}
