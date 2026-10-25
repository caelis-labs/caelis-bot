package websurface

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// nativeReceipt is host-captured evidence for a resident submission. Missing
// legacy provenance, a different owner or another ID never authorizes settlement.
type nativeReceipt struct {
	Owner string `json:"owner"`
	ID    string `json:"id"`
}

// reconcileOperation runs under s.mu. It only reads the original owner's
// authoritative projection; it never dispatches, resumes or retries an effect.
func (s *Server) reconcileOperation(id string, op operation) (operation, error) {
	if operationTerminal(op) || op.State != "complete" || op.Native == nil || op.Native.Owner == "" || op.Native.ID == "" {
		return op, nil
	}
	v := s.services.Backend.CurrentRuntimeBinding().Engine.Snapshot()
	receipt := v.LastReceipt
	if v.RuntimeOwner != op.Native.Owner || receipt.ID != op.Native.ID || (receipt.Outcome != "accepted" && receipt.Outcome != "rejected") {
		return op, nil
	}
	next := op
	next.Value, _ = json.Marshal(receipt)
	next.Error = ""
	next.Detail = ""
	s.operations[id] = next
	if err := s.saveOperations(); err != nil {
		s.operations[id] = op
		return op, err
	}
	return next, nil
}

// Resolve the original pairing proof outside the Web journal lock. This only
// queries the machines owner; the old ingress ID is never dispatched again.
func (s *Server) reconcileBotConnection(ctx context.Context, id, owner string) error {
	s.mu.Lock()
	op, ok := s.operations[id]
	if !ok || op.Owner != owner || op.State != "complete" || op.Error != "operation_unconfirmed" || op.BotConnection == nil {
		s.mu.Unlock()
		return nil
	}
	ref := *op.BotConnection
	s.mu.Unlock()
	machine, err := s.services.Backend.ReadBotConnectionReceipt(ctx, ref)
	var rejected *api.ActionRejectedError
	if err != nil && !(errors.As(err, &rejected) && rejected.Code == "pairing_request_not_dispatched") {
		return nil
	} // uncertainty remains uncertainty
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.operations[id]
	if !ok || current.Owner != owner || current.Digest != op.Digest || current.State != op.State || current.Error != op.Error || current.BotConnection == nil || *current.BotConnection != ref {
		return nil
	}
	next := current
	next.Value, _ = json.Marshal(machine)
	next.Error, next.Detail = "", ""
	if rejected != nil {
		next.Value = nil
		next.Error = "operation_rejected"
		next.Detail = safeRejectionDetail(rejected.Code)
	}
	s.operations[id] = next
	if err = s.saveOperations(); err != nil {
		s.operations[id] = current
		return err
	}
	return nil
}
