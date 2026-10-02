package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ConfigureSetup(v api.SetupController) { s.setup = v }
func (s *Service) SetupOverview() api.SetupOverview {
	if s.setup == nil {
		return api.SetupOverview{}
	}
	return s.setup.Overview()
}
func (s *Service) SetupProfile(id string) (api.RuntimeSettings, error) {
	if s.setup == nil {
		return api.RuntimeSettings{}, errors.New("运行时管理不可用")
	}
	return s.setup.Profile(id)
}
func (s *Service) InspectSetup(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	if s.setup == nil {
		return api.SetupState{}, errors.New("运行时管理不可用")
	}
	return s.setup.Inspect(ctx, v)
}
func (s *Service) SetupCatalog(ctx context.Context, v api.SetupRequest) ([]api.SetupChoice, error) {
	if s.setup == nil {
		return nil, errors.New("运行时管理不可用")
	}
	return s.setup.Catalog(ctx, v)
}
func (s *Service) ApplySetup(ctx context.Context, v api.SetupRequest) (api.SetupState, error) {
	if s.setup == nil {
		return api.SetupState{}, errors.New("运行时管理不可用")
	}
	return s.setup.Apply(ctx, v)
}
func (s *Service) ActivateRuntime(ctx context.Context, v api.RuntimeSettings) error {
	if s.setup == nil {
		return errors.New("运行时管理不可用")
	}
	return s.setup.Activate(ctx, v)
}
func (s *Service) DismissSetup() error {
	if s.setup == nil {
		return nil
	}
	return s.setup.Dismiss()
}

// Freeze new conversation admission before the native host begins relaunch.
func (s *Service) PrepareRestart(guard func() error) error {
	s.admission.Lock()
	defer s.admission.Unlock()
	if s.restarting {
		return errors.New("正在重新启动")
	}
	if e := guard(); e != nil {
		return e
	}
	s.restarting = true
	return nil
}
func (s *Service) CancelRestart()      { s.admission.Lock(); s.restarting = false; s.admission.Unlock() }
func (s *Service) RequireSetup(v bool) { s.admission.Lock(); s.setupRequired = v; s.admission.Unlock() }
