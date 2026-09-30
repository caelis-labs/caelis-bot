package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

// Binding fences an inspected native connection. It contains no credential or
// native identity, and cannot select another Bot, service, or filesystem root.
type RemoteRuntimeState struct {
	Binding      string                              `json:"binding"`
	Label        string                              `json:"label"`
	Available    bool                                `json:"available"`
	Capabilities productmanagement.Capabilities      `json:"capabilities"`
	Releases     []productmanagement.ReviewedRelease `json:"releases"`
	Pending      []RemoteRuntimePending              `json:"pending"`
}

type RemoteRuntimePending struct {
	ID      string                `json:"id"`
	Kind    string                `json:"kind"`
	Runtime *RemoteRuntimeRequest `json:"runtime,omitempty"`
}

type RemoteRuntimeRequest struct {
	ID              string `json:"id"`
	Binding         string `json:"binding"`
	Action          string `json:"action"`
	Runtime         string `json:"runtime"`
	Version         string `json:"version"`
	ExpectedVersion string `json:"expectedVersion"`
}

type RemoteConfigurationRequest struct {
	ID      string                         `json:"id"`
	Binding string                         `json:"binding"`
	Change  api.RuntimeConfigurationChange `json:"change"`
}

type RemoteManagementResult struct {
	ID            string                     `json:"id"`
	Outcome       string                     `json:"outcome"`
	Code          string                     `json:"code"`
	Status        *runtimemanagement.Status  `json:"status,omitempty"`
	Configuration *api.RuntimeMutationResult `json:"configuration,omitempty"`
}

type RemoteManagementController interface {
	RemoteRuntime(context.Context) (RemoteRuntimeState, error)
	RemoteRuntimeStatus(context.Context, string, string) (runtimemanagement.Status, error)
	RemoteRuntimeConfiguration(context.Context, string) (api.RuntimeConfiguration, error)
	ManageRemoteRuntime(context.Context, RemoteRuntimeRequest) (RemoteManagementResult, error)
	ChangeRemoteRuntimeConfiguration(context.Context, RemoteConfigurationRequest) (RemoteManagementResult, error)
	ReconcileRemoteManagement(context.Context, string, string) (RemoteManagementResult, error)
}

func (s *Service) ConfigureRemoteManagement(controller RemoteManagementController) {
	s.mu.Lock()
	s.remoteManagement = controller
	s.mu.Unlock()
}

func (s *Service) remoteManagementController() (RemoteManagementController, error) {
	s.mu.Lock()
	controller := s.remoteManagement
	s.mu.Unlock()
	if controller == nil {
		return nil, errors.New("remote Runtime management is unavailable")
	}
	return controller, nil
}

func (s *Service) RemoteRuntime(ctx context.Context) (RemoteRuntimeState, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteRuntimeState{}, nil
	}
	return controller.RemoteRuntime(ctx)
}
func (s *Service) RemoteRuntimeStatus(ctx context.Context, binding, runtime string) (runtimemanagement.Status, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return runtimemanagement.Status{}, err
	}
	return controller.RemoteRuntimeStatus(ctx, binding, runtime)
}
func (s *Service) RemoteRuntimeConfiguration(ctx context.Context, binding string) (api.RuntimeConfiguration, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	return controller.RemoteRuntimeConfiguration(ctx, binding)
}
func (s *Service) ManageRemoteRuntime(ctx context.Context, request RemoteRuntimeRequest) (RemoteManagementResult, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteManagementResult{}, err
	}
	return controller.ManageRemoteRuntime(ctx, request)
}
func (s *Service) ChangeRemoteRuntimeConfiguration(ctx context.Context, request RemoteConfigurationRequest) (RemoteManagementResult, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteManagementResult{}, err
	}
	return controller.ChangeRemoteRuntimeConfiguration(ctx, request)
}
func (s *Service) ReconcileRemoteManagement(ctx context.Context, binding, id string) (RemoteManagementResult, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteManagementResult{}, err
	}
	return controller.ReconcileRemoteManagement(ctx, binding, id)
}
