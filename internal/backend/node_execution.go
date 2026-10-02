package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) nodeExecutionLocked(ctx context.Context) (string, api.ExecutionSettings, api.WorkExecutionSettings, error) {
	conversation, err := s.executionSettingsLocked(ctx)
	if err != nil {
		return "", conversation, api.WorkExecutionSettings{}, err
	}
	worker := s.workExecutionSettings
	data, _ := json.Marshal(struct {
		Conversation api.ExecutionSettings
		Worker       api.WorkExecutionSettings
	}{conversation, worker})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), conversation, worker, nil
}

// NodeExecutionScopes captures both existing local preferences under their
// configuration mutex. It does not start a Runtime or change its active owner.
func (s *Service) NodeExecutionScopes(ctx context.Context) (string, api.WorkExecutionSettings, api.WorkExecutionSettings, error) {

	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	rev, conversation, worker, err := s.nodeExecutionLocked(ctx)
	return rev, api.WorkExecutionSettings{Model: conversation.Model, Effort: conversation.Effort, ServiceTier: conversation.ServiceTier}, worker, err
}

// ChangeNodeExecutionScopes performs the displayed revision CAS under the same
// mutex used by existing preferences. Permissions are retained; scoped model
// changes cannot introduce a new approval policy or expand service-tier support.
func (s *Service) ChangeNodeExecutionScopes(ctx context.Context, r api.NodeManagementRequest) (api.RuntimeMutationResult, error) {

	result := api.RuntimeMutationResult{OperationID: r.Ref.OperationID, Outcome: "rejected"}
	if r.Ref.NodeID != api.LocalNodeID || r.Ref.Backend != api.NodeCodex || r.Change == nil || (r.Change.Action != "conversation-model" && r.Change.Action != "worker-model") {
		return result, nil
	}
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	active := s.runtimeSettings.Runtime
	if p, ok := s.engine.(api.Provider); ok {
		active = p.ProviderInfo().ID
	}
	if active != "codex" {
		return result, errors.New("local active Runtime is not Codex")
	}
	rev, conversation, worker, err := s.nodeExecutionLocked(ctx)
	if err != nil {
		return result, err
	}
	if rev != r.Guard.Revision || rev != r.Change.ExpectedRevision {
		result.Outcome = "conflicted"
		return result, nil
	}
	selection := r.Change.Selection
	if err := api.ValidateExecutionSettings(selection.Execution()); err != nil {
		return result, nil
	}
	if selection.ServiceTier != "" {
		models, e := s.Models(ctx)
		if e != nil {
			return result, e
		}
		if !slices.ContainsFunc(models, func(m api.ModelOption) bool {
			return m.Model == selection.Model && slices.ContainsFunc(m.ServiceTiers, func(t api.ServiceTier) bool { return t.ID == selection.ServiceTier })
		}) {
			return result, nil
		}
	}

	if r.Change.Action == "conversation-model" {
		conversation.Model, conversation.Effort, conversation.ServiceTier = selection.Model, selection.Effort, selection.ServiceTier
		err = s.saveExecutionSettingsLocked(ctx, conversation)
	} else {
		worker.Model, worker.Effort, worker.ServiceTier = selection.Model, selection.Effort, selection.ServiceTier
		err = s.saveWorkExecutionSettingsLocked(ctx, worker)
	}
	if err != nil {
		result.Outcome = "unknown"
		return result, err
	}
	result.Outcome = "committed"
	return result, nil
}
