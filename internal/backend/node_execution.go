package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

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
	if s.RuntimeSettings().Runtime != "codex" {
		return result, errors.New("local active Runtime is not Codex")
	}
	s.configurationMu.Lock()
	defer s.configurationMu.Unlock()
	rev, conversation, worker, err := s.nodeExecutionLocked(ctx)
	if err != nil {
		return result, err
	}
	if rev != r.Guard.Revision || rev != r.Change.ExpectedRevision {
		result.Outcome = "conflicted"
		return result, nil
	}
	selection := r.Change.Selection
	if r.Change.Action == "conversation-model" {
		if selection.ServiceTier != "" && selection.ServiceTier != conversation.ServiceTier {
			return result, nil
		}
		conversation.Model, conversation.Effort = selection.Model, selection.Effort
		err = s.saveExecutionSettingsLocked(ctx, conversation)
	} else {
		if selection.ServiceTier != "" && selection.ServiceTier != worker.ServiceTier {
			return result, nil
		}
		worker.Model, worker.Effort = selection.Model, selection.Effort
		err = s.saveWorkExecutionSettingsLocked(ctx, worker)
	}
	if err != nil {
		result.Outcome = "unknown"
		return result, err
	}
	result.Outcome = "committed"
	return result, nil
}
