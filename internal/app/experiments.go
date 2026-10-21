package app

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

func (s *runtimeSetup) RuntimeExperiments(ctx context.Context) (api.RuntimeExperiments, error) {
	p, err := s.Profile("caelis")
	if err != nil {
		return api.RuntimeExperiments{}, err
	}
	dir, err := providerDirectory(s.app.root, "caelis")
	if err != nil {
		return api.RuntimeExperiments{}, err
	}
	return caelis.ReadRuntimeExperiments(ctx, p, dir)
}
func (s *runtimeSetup) ChangeRuntimeExperiments(ctx context.Context, v api.RuntimeExperimentChange) (api.RuntimeExperiments, error) {
	p, err := s.Profile("caelis")
	if err != nil {
		return api.RuntimeExperiments{}, &api.ActionRejectedError{Code: "experimental_settings_unavailable"}
	}
	dir, err := providerDirectory(s.app.root, "caelis")
	if err != nil {
		return api.RuntimeExperiments{}, &api.ActionRejectedError{Code: "experimental_settings_unavailable"}
	}
	return caelis.ChangeRuntimeExperiments(ctx, p, dir, v)
}
