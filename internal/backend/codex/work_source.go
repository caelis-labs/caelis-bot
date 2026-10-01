package codex

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// WorkDispatchSource attests the same native activation used by local task
// admission. Codex's current binding does not retain an exact user/background
// source kind, so this port explicitly reports native_activation. It never
// returns the delegation prompt, infers consent from prose, or starts work.
func (s *Session) WorkDispatchSource(ctx context.Context) (api.WorkDispatchSource, error) {
	if err := ctx.Err(); err != nil {
		return api.WorkDispatchSource{}, err
	}
	s.mu.Lock()
	if err := s.taskAdmission(); err != nil {
		s.mu.Unlock()
		return api.WorkDispatchSource{}, err
	}
	source := api.WorkDispatchSource{NodeID: api.LocalNodeID, Backend: "codex", BindingID: s.binding.ThreadID, OperationID: s.run, Kind: "native_activation"}
	annotate := s.opts.DispatchSource
	s.mu.Unlock()
	if annotate != nil {
		return annotate(ctx, source)
	}
	return source, source.Validate()
}

// ConfigureDispatchSource is native assembly only, before activation.
func (s *Session) ConfigureDispatchSource(annotate func(context.Context, api.WorkDispatchSource) (api.WorkDispatchSource, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts.DispatchSource = annotate
}

var _ api.WorkSourceProvider = (*Session)(nil)
