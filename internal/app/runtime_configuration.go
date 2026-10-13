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
func (s *runtimeSetup) workerTeamProfile() (api.RuntimeSettings, error) {
	if s.app.localWork == nil {
		return api.RuntimeSettings{}, errors.New("本机工作设置尚未就绪")
	}
	s.app.localWork.mu.Lock()
	runtime := s.app.localWork.defaultRuntime
	s.app.localWork.mu.Unlock()
	if runtime != "caelis" {
		return api.RuntimeSettings{}, errors.New("请先选择 Caelis Worker，再配置 Team")
	}
	profile, err := s.Profile("caelis")
	if err != nil {
		return profile, err
	}
	return profile, validSetup(profile)
}
func (s *runtimeSetup) WorkerTeamConfiguration(ctx context.Context) (api.RuntimeConfiguration, error) {
	profile, err := s.workerTeamProfile()
	if err != nil {
		return api.RuntimeConfiguration{}, err
	}
	return caelis.ReadRuntimeConfiguration(ctx, profile)
}
func (s *runtimeSetup) ChangeWorkerTeamConfiguration(ctx context.Context, change api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	switch change.Action {
	case "bind", "reset", "create-role", "delete-role", "save-set", "apply-set", "delete-set":
	default:
		return api.RuntimeMutationResult{}, errors.New("这里只能修改 Caelis Team")
	}
	profile, err := s.workerTeamProfile()
	if err != nil {
		return api.RuntimeMutationResult{}, err
	}
	if err := s.app.guardRuntimeChange(); err != nil {
		return api.RuntimeMutationResult{}, err
	}
	return caelis.ChangeRuntimeConfiguration(ctx, profile, change)
}
func (s *runtimeSetup) ChangeRuntimeConfiguration(ctx context.Context, r api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	v, e := s.caelisProfile()
	if e != nil {
		return api.RuntimeMutationResult{}, e
	}
	if r.Action == "remove-model-and-stop" {
		return s.removeModelAndStop(ctx, v, r)
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

// The Host owns the shared model deletion (including the current default).
// Only after a confirmed Host commit do we clear Bot-owned preferences. A
// failure after that commit is reported as unknown so the UI reconciles it
// instead of repeating the destructive Host command.
func (s *runtimeSetup) removeModelAndStop(ctx context.Context, profile api.RuntimeSettings, change api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	if err := s.app.guardRuntimeChange(); err != nil {
		return api.RuntimeMutationResult{}, err
	}
	configuration, err := caelis.ReadRuntimeConfiguration(ctx, profile)
	if err != nil {
		return api.RuntimeMutationResult{}, err
	}
	if configuration.Revision != change.ExpectedRevision {
		return api.RuntimeMutationResult{Outcome: "conflicted", Message: "Caelis 配置已变化，请刷新并重新确认。"}, nil
	}
	found := false
	for _, group := range configuration.Connections {
		if group.Kind != "provider" {
			continue
		}
		for _, model := range group.Models {
			if model.ID != change.ID {
				continue
			}
			found = true
			for _, use := range model.Uses {
				if use != "Caelis 主模型" {
					return api.RuntimeMutationResult{Outcome: "rejected", Message: "Caelis Team 仍在使用此模型，请先解除或更换对应绑定。"}, nil
				}
			}
		}
	}
	if !found {
		return api.RuntimeMutationResult{Outcome: "conflicted", Message: "模型连接已变化，请刷新并重新确认。"}, nil
	}
	dir, _ := providerDirectory(s.app.root, "caelis")
	preferences, err := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
	if err != nil {
		return api.RuntimeMutationResult{}, err
	}
	work := s.app.Backend.WorkExecutionSettings()
	hostChange := change
	hostChange.Action = "remove-model"
	result, err := caelis.ChangeRuntimeConfiguration(ctx, profile, hostChange)
	if err != nil || result.Outcome != "committed" {
		return result, err
	}
	var incomplete bool
	if preferences.Model == change.ID {
		if err := s.app.Backend.ClearDeletedModelPreference(ctx, change.ID); err != nil {
			incomplete = true
		}
	}
	if work.Model == change.ID {
		if err := s.app.Backend.SaveWorkExecutionSettings(ctx, api.WorkExecutionSettings{}); err != nil {
			incomplete = true
		}
	}
	if incomplete {
		result.Outcome = "unknown"
		result.Message = "Caelis 已移除模型，但 Bot 设置尚未完全同步。请刷新连接并核对后再操作。"
	}
	return result, nil
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
