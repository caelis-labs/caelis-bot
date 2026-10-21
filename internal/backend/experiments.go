package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type experimentController interface {
	RuntimeExperiments(context.Context) (api.RuntimeExperiments, error)
	ChangeRuntimeExperiments(context.Context, api.RuntimeExperimentChange) (api.RuntimeExperiments, error)
}

func (s *Service) RuntimeExperiments(ctx context.Context) (api.RuntimeExperiments, error) {
	c, ok := s.setup.(experimentController)
	if !ok {
		return api.RuntimeExperiments{}, errors.New("experimental settings unavailable")
	}
	return c.RuntimeExperiments(ctx)
}
func (s *Service) ChangeRuntimeExperiments(ctx context.Context, v api.RuntimeExperimentChange) (api.RuntimeExperiments, error) {
	c, ok := s.setup.(experimentController)
	if !ok {
		return api.RuntimeExperiments{}, &api.ActionRejectedError{Code: "experimental_settings_unavailable"}
	}
	return c.ChangeRuntimeExperiments(ctx, v)
}
