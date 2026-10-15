package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type runtimeSelectionController interface {
	SetupModels(context.Context, string) ([]api.ModelOption, error)
	SaveSelectedModel(context.Context, string, string, api.WorkExecutionSettings) error
	ProfileConfiguration(context.Context, string) (api.RuntimeConfiguration, error)
	ChangeProfileConfiguration(context.Context, string, api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error)
}

func (s *Service) selectionController() (runtimeSelectionController, error) {
	c, ok := s.setup.(runtimeSelectionController)
	if !ok {
		return nil, errors.New("runtime settings unavailable")
	}
	return c, nil
}
func (s *Service) SetupModels(ctx context.Context, id string) ([]api.ModelOption, error) {
	c, e := s.selectionController()
	if e != nil {
		return nil, e
	}
	return c.SetupModels(ctx, id)
}
func (s *Service) SaveSelectedModel(ctx context.Context, scope, id string, v api.WorkExecutionSettings) error {
	c, e := s.selectionController()
	if e != nil {
		return e
	}
	if e = c.SaveSelectedModel(ctx, scope, id, v); e != nil {
		return e
	}
	if scope == "work" && s.RuntimeSettings().Runtime == id {
		s.configurationMu.Lock()
		s.workExecutionSettings = v
		s.configurationMu.Unlock()
	}
	return nil
}
func (s *Service) ProfileConfiguration(ctx context.Context, id string) (api.RuntimeConfiguration, error) {
	c, e := s.selectionController()
	if e != nil {
		return api.RuntimeConfiguration{}, e
	}
	return c.ProfileConfiguration(ctx, id)
}
func (s *Service) ChangeProfileConfiguration(ctx context.Context, id string, v api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	c, e := s.selectionController()
	if e != nil {
		return api.RuntimeMutationResult{}, e
	}
	return c.ChangeProfileConfiguration(ctx, id, v)
}

func (s *Service) RuntimeReviewMode(id string) (string, error) {
	c, ok := s.setup.(interface{ RuntimeReviewMode(string) (string, error) })
	if !ok {
		return "", errors.New("runtime settings unavailable")
	}
	return c.RuntimeReviewMode(id)
}
func (s *Service) EnableRuntimeAutoReview(ctx context.Context, id string) error {
	c, ok := s.setup.(interface {
		EnableRuntimeAutoReview(context.Context, string) error
	})
	if !ok {
		return errors.New("runtime settings unavailable")
	}
	return c.EnableRuntimeAutoReview(ctx, id)
}

func (s *Service) LocalWorkerSelection() (api.LocalWorkerSettings, error) {
	c, ok := s.localWorkers.(interface {
		LocalWorkerSelection() (api.LocalWorkerSettings, error)
	})
	if !ok {
		return api.LocalWorkerSettings{}, errors.New("worker settings unavailable")
	}
	return c.LocalWorkerSelection()
}
