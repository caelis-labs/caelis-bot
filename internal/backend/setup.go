package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ConfigureSetup(v api.SetupController) {
	s.mu.Lock()
	s.setup = v
	s.mu.Unlock()
}
func (s *Service) setupController() api.SetupController {

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setup
}
func (s *Service) SetupOverview() api.SetupOverview {
	controller := s.setupController()
	if controller == nil {
		return api.SetupOverview{}
	}
	return controller.Overview()
}
func (s *Service) SetupProfile(id string) (api.RuntimeSettings, error) {
	controller := s.setupController()
	if controller == nil {
		return api.RuntimeSettings{}, errors.New("运行时管理不可用")
	}
	return controller.Profile(id)
}
func (s *Service) InspectSetup(ctx context.Context, v api.RuntimeSettings) (api.SetupState, error) {
	controller := s.setupController()
	if controller == nil {
		return api.SetupState{}, errors.New("运行时管理不可用")
	}
	return controller.Inspect(ctx, v)
}
func (s *Service) SetupCatalog(ctx context.Context, v api.SetupRequest) ([]api.SetupChoice, error) {
	controller := s.setupController()
	if controller == nil {
		return nil, errors.New("运行时管理不可用")
	}
	return controller.Catalog(ctx, v)
}
func (s *Service) ApplySetup(ctx context.Context, v api.SetupRequest) (api.SetupState, error) {
	controller := s.setupController()
	if controller == nil {
		return api.SetupState{}, errors.New("运行时管理不可用")
	}
	return controller.Apply(ctx, v)
}
func (s *Service) ActivateRuntime(ctx context.Context, v api.RuntimeSettings) error {
	controller := s.setupController()
	if controller == nil {
		return errors.New("运行时管理不可用")
	}
	return controller.Activate(ctx, v)
}
func (s *Service) DismissSetup() error {

	controller := s.setupController()
	if controller == nil {
		return nil
	}
	return controller.Dismiss()
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
