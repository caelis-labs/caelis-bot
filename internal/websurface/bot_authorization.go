package websurface

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const botAuthorizationPath = "/api/v1/machines/authorize-bot"
const botConnectionPath = "/api/v1/machines/connect-bot"
const botAuthorizationDepth = 16

type botAuthorizationCallKey struct{}
type botAuthorizationCall struct {
	parent    string
	reference api.BotConnectionReference
}
type botRecovery struct {
	OperationID  string `json:"operationId"`
	ProfileID    string `json:"profileId"`
	CanAuthorize bool   `json:"canAuthorize"`
}

// Under the existing ingress lock, resolve only this Web owner's same-profile
// proof chain. No inferred execution outcome or credential leaves this journal.
func (s *Server) botAuthorizationTailLocked(id, owner string) (string, operation, bool) {
	seen := map[string]bool{}
	original, ok := s.operations[id]
	if !ok || original.Owner != owner || original.BotConnection == nil {
		return "", operation{}, false
	}
	ref := *original.BotConnection
	for range botAuthorizationDepth {
		if seen[id] {
			return "", operation{}, false
		}
		seen[id] = true
		op, ok, lookupErr := s.lookupOperation(id)
		if lookupErr != nil || !ok || op.Owner != owner || op.BotConnection == nil || op.BotConnection.ID != ref.ID || op.BotConnection.Proof != ref.Proof {
			return "", operation{}, false
		}
		if op.AuthorizationBy == "" {
			return id, op, true
		}
		child, ok, childErr := s.lookupOperation(op.AuthorizationBy)
		if childErr != nil || !ok || !child.BotAuthorization || child.AuthorizationParent != id {
			return "", operation{}, false
		}
		if operationTerminal(child) && child.Error != "" && !botAuthorizationConfirmed(child) {
			return id, op, true
		} // known refusal did not replace original uncertainty
		id = op.AuthorizationBy
	}
	return "", operation{}, false
}
func (s *Server) prepareBotAuthorizationLocked(path string, in actionRequest, owner string) (*botAuthorizationCall, error) {
	if path == botAuthorizationPath {
		if len(in.Args) != 1 {
			return nil, &api.ActionRejectedError{Code: "invalid_argument"}
		}
		var input api.BotConnectionAuthorization
		if json.Unmarshal(in.Args[0], &input) != nil {
			return nil, &api.ActionRejectedError{Code: "invalid_argument"}
		}
		if input.OriginalOperationID == "" {
			// Web recovery always names its original authenticated receipt. Desktop
			// uses the direct controller; an unlinked Web result cannot open a gate.
			return nil, &api.ActionRejectedError{Code: "original_peer_proof_required"}
		}
		id, op, ok := s.botAuthorizationTailLocked(input.OriginalOperationID, owner)
		if !ok || id != input.OriginalOperationID || op.State != "complete" || op.Error != "operation_unconfirmed" {
			return nil, errors.New("original authorization not settled")
		}
		if input.Connection.ID != "" && input.Connection.ID != op.BotConnection.ID {
			return nil, &api.ActionRejectedError{Code: "original_bot_authorization_changed"}
		}
		// Every uncertain ancestor retains its original code digest. A fresh code
		// cannot merely be a different URL/name/ingress ID for any of those writes.
		current := id
		for range botAuthorizationDepth {
			parent := s.operations[current]
			if parent.BotConnection == nil {
				return nil, errors.New("original proof unavailable")
			}
			if err := backend.ValidateBotAuthorizationCode(s.services.Backend, *parent.BotConnection, input.Connection.Code); err != nil {
				return nil, err
			}
			if parent.AuthorizationParent == "" {
				break
			}
			current = parent.AuthorizationParent
		}
		// The old receipt keeps its original epoch. This distinct authorization
		// snapshots the current generation of the same retained proof, including an
		// earlier write whose process died before its Web result could be saved.
		ref, err := backend.BotAuthorizationReference(s.services.Backend, op.BotConnection.ID)
		if err != nil || ref.Proof != op.BotConnection.Proof {
			return nil, &api.ActionRejectedError{Code: "original_peer_proof_required"}
		}
		prepared, err := backend.PrepareBotConnection(s.services.Backend, input.Connection, &ref)
		if err != nil {
			return nil, err
		}
		return &botAuthorizationCall{parent: id, reference: prepared}, nil
	}
	if path != botConnectionPath {
		return nil, &api.ActionRejectedError{Code: "invalid_argument"}
	}
	op, ok, lookupErr := s.lookupOperation(in.AfterAuthorization)
	if in.AfterAuthorization != "" && (lookupErr != nil || !ok || op.Owner != owner || !botAuthorizationConfirmed(op) || op.BotConnection == nil) {
		return nil, errors.New("authorization unconfirmed")
	}
	// Following permits another explicit connection intent; it does not certify
	// any earlier unknown. Prevent replay of an ancestor's original pairing code.
	var input api.BotConnectionInput
	if len(in.Args) != 1 || json.Unmarshal(in.Args[0], &input) != nil {
		return nil, &api.ActionRejectedError{Code: "invalid_argument"}
	}
	if input.Code != "" {
		id := op.AuthorizationParent
		for depth := 0; id != "" && depth < botAuthorizationDepth; depth++ {
			parent, ok := s.operations[id]
			if !ok || parent.Owner != owner || parent.BotConnection == nil {
				return nil, errors.New("original proof unavailable")
			}
			if err := backend.ValidateBotAuthorizationCode(s.services.Backend, *parent.BotConnection, input.Code); err != nil {
				return nil, err
			}
			id = parent.AuthorizationParent
		}
	}
	ref, err := backend.PrepareBotConnection(s.services.Backend, input, nil)
	if err != nil {
		return nil, err
	}
	return &botAuthorizationCall{reference: ref}, nil
}
func (s *Server) authorizeBotConnection(ctx context.Context, in api.BotConnectionAuthorization) (api.Machine, error) {
	call, ok := ctx.Value(botAuthorizationCallKey{}).(*botAuthorizationCall)
	if !ok {
		return api.Machine{}, errors.New("original authorization unavailable")
	}
	value, ref, err := backend.ConnectPreparedBot(s.services.Backend, call.reference, in.Connection)
	call.reference = ref
	return value, err
}

func botAuthorizationConfirmed(op operation) bool {
	return op.BotAuthorization && op.State == "complete" && (op.ArchivedAuthorization || operationTerminal(op) && op.Error == "")
}

// The pairing domain decides whether an actual invocation remains active.
// Historical unknown authorization/epoch changes never govern local health.
func (s *Server) botAuthorizationAllowsRestartLocked(id string, op operation) bool {
	return op.State == "complete" && op.BotConnection != nil && op.Native == nil && backend.BotConnectionQuiescent(s.services.Backend, *op.BotConnection)
}

func (s *Server) connectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	call, ok := ctx.Value(botAuthorizationCallKey{}).(*botAuthorizationCall)
	if !ok {
		return api.Machine{}, errors.New("original authorization unavailable")
	}
	value, ref, err := backend.ConnectPreparedBot(s.services.Backend, call.reference, in)
	call.reference = ref
	return value, err
}
