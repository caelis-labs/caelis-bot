package websurface

import "encoding/json"

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
	s.operations[id] = next
	if err := s.saveOperations(); err != nil {
		s.operations[id] = op
		return op, err
	}
	return next, nil
}
