package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) runtimeConfigurationController() (api.RuntimeConfigurationController, error) {
	controller, ok := s.setup.(api.RuntimeConfigurationController)
	if !ok {
		return nil, errors.New("运行时配置不可用")
	}
	return controller, nil
}
func (s *Service) RuntimeConfiguration(ctx context.Context) (api.RuntimeConfiguration, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeConfiguration{}, e
	}
	return c.RuntimeConfiguration(ctx)
}
func (s *Service) ChangeRuntimeConfiguration(ctx context.Context, r api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeMutationResult{}, e
	}
	return c.ChangeRuntimeConfiguration(ctx, r)
}
func (s *Service) RuntimeConnectionCatalog(ctx context.Context, kind string, settings *api.RuntimeSettings) (api.RuntimeConnectionCatalog, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeConnectionCatalog{}, e
	}
	return c.RuntimeConnectionCatalog(ctx, kind, settings)
}
func (s *Service) StartRuntimeConnection(ctx context.Context, r api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.StartRuntimeConnection(ctx, r)
}
func (s *Service) AdvanceRuntimeConnection(ctx context.Context, r api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.AdvanceRuntimeConnection(ctx, r)
}
func (s *Service) WaitRuntimeConnection(ctx context.Context, id string, after int) (api.RuntimeFlow, error) {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.WaitRuntimeConnection(ctx, id, after)
}
func (s *Service) CancelRuntimeConnection(ctx context.Context, id string) error {
	c, e := s.runtimeConfigurationController()
	if e != nil {
		return e
	}
	return c.CancelRuntimeConnection(ctx, id)
}
