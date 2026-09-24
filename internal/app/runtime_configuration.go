package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
)

func (s *runtimeSetup) caelisProfile() (api.RuntimeSettings, error) {
	profile := s.app.Backend.RuntimeSettings()
	if profile.Runtime != "caelis" {
		return profile, errors.New(s.app.text("host.switchToCaelisFirst"))
	}
	return profile, validSetup(profile)
}
func (s *runtimeSetup) RuntimeConfiguration(ctx context.Context) (api.RuntimeConfiguration, error) {
	v, e := s.caelisProfile()
	if e != nil {
		return api.RuntimeConfiguration{}, e
	}
	return caelis.ReadRuntimeConfiguration(ctx, v)
}
func (s *runtimeSetup) ChangeRuntimeConfiguration(ctx context.Context, r api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	v, e := s.caelisProfile()
	if e != nil {
		return api.RuntimeMutationResult{}, e
	}
	// Destructive connection changes retain the existing Bot admission guard.
	if r.Action == "remove-model" || r.Action == "disconnect-agent" {
		if e = s.app.guardRuntimeChange(); e != nil {
			return api.RuntimeMutationResult{}, e
		}
		dir, _ := providerDirectory(s.app.root, "caelis")
		prefs, err := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
		if err != nil {
			return api.RuntimeMutationResult{}, err
		}
		worker := s.app.Backend.WorkExecutionSettings()
		configuration, err := caelis.ReadRuntimeConfiguration(ctx, v)
		if err != nil {
			return api.RuntimeMutationResult{}, err
		}
		for _, group := range configuration.Connections {
			for _, model := range group.Models {
				match := r.Action == "remove-model" && model.ID == r.ID || r.Action == "disconnect-agent" && group.Kind == "agent" && group.ID == r.ID
				if match && (model.ID == prefs.Model || model.ID == worker.Model) {
					return api.RuntimeMutationResult{}, errors.New(s.app.text("host.changeBotModelBeforeDisconnect"))
				}
			}
		}
	}
	return caelis.ChangeRuntimeConfiguration(ctx, v, r)
}
func (s *runtimeSetup) connectionProfile(settings *api.RuntimeSettings) (api.RuntimeSettings, error) {
	if settings == nil {
		return s.caelisProfile()
	}
	if settings.Runtime != "caelis" {
		return api.RuntimeSettings{}, errors.New(s.app.text("host.connectionRequiresCaelis"))
	}
	return *settings, validSetup(*settings)
}
func (s *runtimeSetup) RuntimeConnectionCatalog(ctx context.Context, kind string, settings *api.RuntimeSettings) (api.RuntimeConnectionCatalog, error) {
	v, e := s.connectionProfile(settings)
	if e != nil {
		return api.RuntimeConnectionCatalog{}, e
	}
	return caelis.ConnectionCatalog(ctx, v, kind)
}
func (s *runtimeSetup) StartRuntimeConnection(ctx context.Context, r api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	v, e := s.connectionProfile(r.Settings)
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return s.connections.Start(ctx, v, r)
}
func (s *runtimeSetup) AdvanceRuntimeConnection(ctx context.Context, r api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	// A flow pins the explicitly chosen Host, including setup before activation.
	return s.connections.Advance(ctx, r)
}
func (s *runtimeSetup) WaitRuntimeConnection(ctx context.Context, id string, after int) (api.RuntimeFlow, error) {
	return s.connections.Wait(ctx, id, after)
}
func (s *runtimeSetup) CancelRuntimeConnection(ctx context.Context, id string) error {
	return s.connections.Cancel(ctx, id)
}
