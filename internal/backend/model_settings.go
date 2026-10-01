package backend

import (
	"context"
	"errors"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

// ModelSettingsAvailable observes an existing provider; it never opens a Runtime.
func (s *Service) ModelSettingsAvailable() bool {
	_, ok := s.capabilityEngine().(api.ExecutionProvider)
	return ok
}

func (s *Service) modelSettingsLocked(ctx context.Context) (productmanagement.ExecutionState, error) {
	v, err := s.executionSettingsLocked(ctx)
	activeProvider := s.runtimeSettings.Runtime
	if p, ok := s.capabilityEngine().(api.Provider); ok {
		activeProvider = p.ProviderInfo().ID
	}
	state := productmanagement.ExecutionState{Conversation: v, ConversationDefault: activeProvider == "caelis"}
	if _, ok := s.capabilityEngine().(api.WorkExecutionProvider); ok {
		work := s.workExecutionSettings
		state.Work = &work
	}
	return state, err
}

func (s *Service) ReadModelSettings(ctx context.Context) (productmanagement.ExecutionState, []api.ModelOption, error) {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if !s.ModelSettingsAvailable() {
		return productmanagement.ExecutionState{}, nil, productmanagement.ErrExecutionUnavailable
	}
	state, err := s.modelSettingsLocked(ctx)
	if err != nil {
		return state, nil, err
	}
	models, err := s.Models(ctx)
	return state, models, err
}

// Compare, patch and existing validation/persistence share the local settings
// mutex. A racing local save cannot silently replace permissions or tier.
func (s *Service) ApplyModelSettings(ctx context.Context, revision, target string, selection productmanagement.Selection) error {
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	if !s.ModelSettingsAvailable() {
		return productmanagement.ErrExecutionUnavailable
	}
	state, err := s.modelSettingsLocked(ctx)
	if err != nil {
		return errors.Join(productmanagement.ErrExecutionUnavailable, err)
	}
	if productmanagement.ExecutionRevision(state) != revision {
		return productmanagement.ErrExecutionConflict
	}
	if target != "conversation" && target != "work" {
		return productmanagement.ErrExecutionInvalid
	}
	if target == "work" && state.Work == nil {
		return productmanagement.ErrExecutionUnavailable
	}
	// Reject malformed/current-catalog-invalid choices before native dispatch.
	if api.ValidateExecutionSettings(api.ExecutionSettings{Model: selection.Model, Effort: selection.Effort}) != nil {
		return productmanagement.ErrExecutionInvalid
	}
	catalog, err := s.Models(ctx)
	if err != nil {
		return errors.Join(productmanagement.ErrExecutionUnavailable, err)
	}
	if target == "conversation" && selection.Model == "" && (!state.ConversationDefault || state.Conversation.ServiceTier != "") {
		return productmanagement.ErrExecutionInvalid
	}
	if selection.Model != "" {
		i := slices.IndexFunc(catalog, func(m api.ModelOption) bool { return m.Model == selection.Model })
		if i < 0 {
			return productmanagement.ErrExecutionInvalid
		}
		m := catalog[i]
		// Caelis conversation configuration supports its native default effort.
		// Codex and work overrides retain their existing explicit-effort policy.
		defaultEffort := target == "conversation" && state.ConversationDefault && selection.Effort == ""
		if !defaultEffort && !slices.Contains(m.Efforts, selection.Effort) && !(selection.Effort == "" && len(m.Efforts) == 0) {
			return productmanagement.ErrExecutionInvalid
		}
	}
	if ctx.Err() != nil {
		return productmanagement.ErrExecutionCancelled
	}
	if target == "work" {
		v := *state.Work
		v.Model, v.Effort = selection.Model, selection.Effort
		if v.Model == "" {
			v.ServiceTier = ""
		}
		if api.ValidateWorkExecution(v, catalog) != nil {
			return productmanagement.ErrExecutionInvalid
		}
		return s.saveWorkExecutionSettingsLocked(ctx, v)
	}
	v := state.Conversation
	v.Model, v.Effort = selection.Model, selection.Effort
	if v.ServiceTier != "" {
		i := slices.IndexFunc(catalog, func(m api.ModelOption) bool { return m.Model == v.Model })
		if i < 0 || !slices.ContainsFunc(catalog[i].ServiceTiers, func(t api.ServiceTier) bool { return t.ID == v.ServiceTier }) {
			return productmanagement.ErrExecutionInvalid
		}
	}
	return s.saveExecutionSettingsLocked(ctx, v)
}
