package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

// Remote execution choices carry only the inspected product binding and public
// catalog. Native scope, permission policy, tiers and credentials stay native.
type RemoteExecutionView struct {
	Binding             string                       `json:"binding"`
	ConversationDefault bool                         `json:"conversationDefault"`
	Conversation        productmanagement.Selection  `json:"conversation"`
	Work                *productmanagement.Selection `json:"work,omitempty"`
	Revision            string                       `json:"revision"`
	Models              []api.ModelOption            `json:"models"`
}

type RemoteExecutionRequest struct {
	ID               string                      `json:"id"`
	Binding          string                      `json:"binding"`
	Target           string                      `json:"target"`
	ExpectedRevision string                      `json:"expectedRevision"`
	Selection        productmanagement.Selection `json:"selection"`
}

type remoteExecutionController interface {
	RemoteExecutionSettings(context.Context, string) (RemoteExecutionView, error)
	ChangeRemoteExecutionSettings(context.Context, RemoteExecutionRequest) (RemoteManagementResult, error)
}

func (s *Service) RemoteExecutionSettings(ctx context.Context, binding string) (RemoteExecutionView, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteExecutionView{}, err
	}
	execution, ok := controller.(remoteExecutionController)
	if !ok {
		return RemoteExecutionView{}, errors.New("remote model settings are unavailable")
	}
	return execution.RemoteExecutionSettings(ctx, binding)
}
func (s *Service) ChangeRemoteExecutionSettings(ctx context.Context, request RemoteExecutionRequest) (RemoteManagementResult, error) {
	controller, err := s.remoteManagementController()
	if err != nil {
		return RemoteManagementResult{}, err
	}
	execution, ok := controller.(remoteExecutionController)
	if !ok {
		return RemoteManagementResult{}, errors.New("remote model settings are unavailable")
	}
	return execution.ChangeRemoteExecutionSettings(ctx, request)
}
