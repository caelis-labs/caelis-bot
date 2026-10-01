package caelis

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Export only metadata after the ordinary resident callback authority check.
// Callback objects and remote enrollment grants never leave this adapter.
func (s *Session) WorkDispatchSource(ctx context.Context) (api.WorkDispatchSource, error) {
	call, err := s.authority(ctx)
	if err != nil {
		return api.WorkDispatchSource{}, err
	}
	source := api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "caelis", BindingID: call.SessionId, OperationID: call.Source.OperationId, Kind: call.Source.Kind}
	s.mu.Lock()
	annotate := s.dispatchSource
	s.mu.Unlock()
	if annotate != nil {
		return annotate(ctx, source)
	}
	return source, source.Validate()
}

var _ api.WorkSourceProvider = (*Session)(nil)
