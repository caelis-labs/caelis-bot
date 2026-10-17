package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"path/filepath"
)

// Catalog inspection never selects a conversation. A confirmed obsolete Worker
// default may fall back to following that conversation.
func (s *runtimeSetup) SetupModels(ctx context.Context, id string) ([]api.ModelOption, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.Profile(id)
	if e != nil {
		return nil, e
	}
	return s.profileModels(ctx, p)
}
func (s *runtimeSetup) profileModels(ctx context.Context, p api.RuntimeSettings) ([]api.ModelOption, error) {
	state, e := s.inspect(ctx, p)
	if e != nil {
		return nil, e
	}
	if state.State != "ready" {
		return []api.ModelOption{}, nil
	}
	if p.Runtime == "codex" {
		models, err := s.codex.Models(ctx, state.Installation.Path)
		if err == nil {
			s.reconcileWorkerModels(p.Runtime, models)
		}
		return models, err
	}
	config, e := caelis.ReadRuntimeConfiguration(ctx, p)
	if e == nil {
		s.reconcileWorkerModels(p.Runtime, config.Models)
	}
	return config.Models, e
}

func (s *runtimeSetup) reconcileWorkerModels(runtime string, models []api.ModelOption) {
	if s.app.localWork != nil {
		if err := s.app.localWork.reconcileModelCatalog(runtime, models); err != nil && s.app.host.ReportError != nil {
			s.app.host.ReportError(err)
		}
	}
}
func (s *runtimeSetup) SaveSelectedModel(ctx context.Context, scope, id string, v api.WorkExecutionSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope != "conversation" && scope != "work" {
		return errors.New("invalid model scope")
	}
	if scope == "work" && id == "conversation" {
		if v != (api.WorkExecutionSettings{}) {
			return errors.New("follow conversation requires empty model parameters")
		}
		return s.app.localWork.SelectFirstRuntime("conversation")
	}
	p, e := s.Profile(id)
	if e != nil {
		return e
	}
	models, e := s.profileModels(ctx, p)
	if e != nil {
		return e
	}
	if v.Model == "" && scope == "conversation" {
		return errors.New("select a model")
	}
	if len(models) == 0 {
		return errors.New("runtime models unavailable")
	}
	if e = api.ValidateWorkExecution(v, models); e != nil {
		return e
	}
	if scope == "work" {
		return s.app.localWork.saveSelectedModel(ctx, id, v)
	}
	if e = s.app.guardRuntimeChange(); e != nil {
		return e
	}
	if s.app.HasRuntimeChoice() && s.app.Backend.RuntimeSettings() == p {
		prefs, err := s.app.Backend.ExecutionSettings()
		if err != nil {
			return err
		}
		prefs.Model = v.Model
		prefs.Effort = v.Effort
		prefs.ServiceTier = v.ServiceTier
		return s.app.Backend.SaveExecutionSettings(ctx, prefs)
	}
	dir, e := providerDirectory(s.app.root, id)
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "execution.json")
	original, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	mode := "workspace-write"
	if id == "codex" {
		mode = "auto"
	}
	prefs, e := backend.LoadExecutionSettings(path, api.ExecutionSettings{ApprovalMode: mode})
	if e != nil {
		return e
	}
	prefs.Model = v.Model
	prefs.Effort = v.Effort
	prefs.ServiceTier = v.ServiceTier
	if e = localstate.Write(path, prefs); e != nil {
		return e
	}
	if e = s.app.activateRuntime(ctx, p, !s.app.HasRuntimeChoice()); e != nil {
		var restore error
		if os.IsNotExist(readErr) {
			restore = os.Remove(path)
		} else {
			restore = localstate.Write(path, json.RawMessage(original))
		}
		return errors.Join(e, restore)
	}
	s.pending = ""
	s.fresh = false
	return nil
}
func (s *runtimeSetup) ProfileConfiguration(ctx context.Context, id string) (api.RuntimeConfiguration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.Profile(id)
	if e != nil {
		return api.RuntimeConfiguration{}, e
	}
	if id != "caelis" {
		return api.RuntimeConfiguration{}, errors.New("provider configuration unavailable")
	}
	config, e := caelis.ReadRuntimeConfiguration(ctx, p)
	if e != nil {
		return config, e
	}
	dir, _ := providerDirectory(s.app.root, id)
	prefs, e := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
	if e != nil {
		return config, e
	}
	work, e := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if e != nil {
		return config, e
	}
	for i := range config.Connections {
		for j := range config.Connections[i].Models {
			model := &config.Connections[i].Models[j]
			if model.ID == prefs.Model {
				model.Uses = append(model.Uses, "bot_conversation")
			}
			if model.ID == work.Model {
				model.Uses = append(model.Uses, "dedicated_work")
			}
		}
	}
	return config, nil
}
func (s *runtimeSetup) ChangeProfileConfiguration(ctx context.Context, id string, v api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	// Only Team actions are available through the profile-scoped surface.
	switch v.Action {
	case "bind", "reset", "delete-role", "create-role", "save-set", "apply-set", "delete-set", "remove-model", "disconnect-agent":
	default:
		return api.RuntimeMutationResult{}, errors.New("unsupported profile change")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, e := s.Profile(id)
	if e != nil {
		return api.RuntimeMutationResult{}, e
	}
	if id != "caelis" {
		return api.RuntimeMutationResult{}, errors.New("team unavailable")
	}
	return s.changeProfileConfiguration(ctx, p, v)
}

func (s *runtimeSetup) RuntimeReviewMode(id string) (string, error) {
	dir, e := providerDirectory(s.app.root, id)
	if e != nil {
		return "", e
	}
	mode := "workspace-write"
	if id == "codex" {
		mode = "auto"
	}
	v, e := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{ApprovalMode: mode})
	return v.ApprovalMode, e
}
func (s *runtimeSetup) EnableRuntimeAutoReview(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, e := providerDirectory(s.app.root, id)
	if e != nil {
		return e
	}
	mode := "workspace-write"
	if id == "codex" {
		mode = "auto"
	}
	if s.app.Backend.RuntimeSettings().Runtime == id {
		v, e := s.app.Backend.ExecutionSettings()
		if e != nil {
			return e
		}
		v.ApprovalMode = mode
		return s.app.Backend.SaveExecutionSettings(ctx, v)
	}
	path := filepath.Join(dir, "execution.json")
	v, e := backend.LoadExecutionSettings(path, api.ExecutionSettings{ApprovalMode: mode})
	if e != nil {
		return e
	}
	v.ApprovalMode = mode
	return localstate.Write(path, v)
}
