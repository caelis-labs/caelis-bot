package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ConfigureRuntimeManagement(providers []api.ProviderInfo, probe func(context.Context, api.RuntimeSettings) error, manage func(context.Context, string, api.RuntimeSettings) (api.RuntimeStatus, error), guard func() error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	s.providers = providers
	s.probeRuntime = probe
	s.manageRuntime = manage
	s.switchGuard = guard
}
func (s *Service) RuntimeProviders() []api.ProviderInfo {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	return append([]api.ProviderInfo(nil), s.providers...)
}
func (s *Service) ManageRuntime(ctx context.Context, action string, settings api.RuntimeSettings) (api.RuntimeStatus, error) {
	s.configurationMu.Lock()
	fn := s.manageRuntime
	guard := s.switchGuard
	s.configurationMu.Unlock()
	if fn == nil {
		return api.RuntimeStatus{}, errors.New("运行时管理不可用")
	}
	if action != "detect" && action != "check-update" && guard != nil && s.engine.Snapshot().Connection == "ready" {
		if e := guard(); e != nil {
			return api.RuntimeStatus{}, e
		}
	}
	return fn(ctx, action, settings)
}
