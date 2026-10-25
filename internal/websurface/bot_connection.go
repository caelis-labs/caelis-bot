package websurface

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const botConnectionPath = "/api/v1/machines/connect-bot"

type botConnectionCallKey struct{}
type botConnectionCall struct{ reference api.BotConnectionReference }

func (s *Server) prepareBotConnectionLocked(in actionRequest) (*botConnectionCall, error) {
	var input api.BotConnectionInput
	if len(in.Args) != 1 || json.Unmarshal(in.Args[0], &input) != nil {
		return nil, &api.ActionRejectedError{Code: "invalid_argument"}
	}
	ref, err := backend.PrepareBotConnection(s.services.Backend, input, nil)
	if err != nil {
		return nil, err
	}
	return &botConnectionCall{reference: ref}, nil
}
func (s *Server) botConnectionAllowsRestartLocked(id string, op operation) bool {
	return op.State == "complete" && op.BotConnection != nil && op.Native == nil && backend.BotConnectionQuiescent(s.services.Backend, *op.BotConnection)
}
func (s *Server) connectBot(ctx context.Context, in api.BotConnectionInput) (api.Machine, error) {
	call, ok := ctx.Value(botConnectionCallKey{}).(*botConnectionCall)
	if !ok {
		return api.Machine{}, errors.New("original connection unavailable")
	}
	value, ref, err := backend.ConnectPreparedBot(s.services.Backend, call.reference, in)
	call.reference = ref
	return value, err
}
