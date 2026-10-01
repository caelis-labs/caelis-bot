package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// OwnedSetupSettings delegates an existing owned Host to native setup code.
// It neither starts nor authenticates a Host, enrolls an application, nor owns
// shutdown. The caller must retain the exact managed owner/generation and check
// it before each SDK action. Shared Hosts never expose this owned-only port.
func (s *Session) OwnedSetupSettings(ctx context.Context) (api.RuntimeSettings, error) {
	if err := ctx.Err(); err != nil {
		return api.RuntimeSettings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.owned == nil {
		return api.RuntimeSettings{}, errors.New("owned Caelis setup delegation unavailable")
	}
	if err := s.owned.check(ctx); err != nil {
		return api.RuntimeSettings{}, readinessFailure("owned-setup-host-unavailable", err)
	}
	return s.owned.settings, nil
}
