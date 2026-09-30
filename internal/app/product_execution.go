package app

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/productmanagement"
)

type nativeProductExecution interface {
	ExecutionSettings(context.Context) (productmanagement.ExecutionView, error)
	ChangeExecutionSettings(context.Context, productmanagement.ExecutionCommand) (productmanagement.ExecutionResult, error)
}

func (e *productEngine) RemoteExecutionSettings(parent context.Context, binding string) (backend.RemoteExecutionView, error) {
	client, managed, scope, err := e.managementClient(binding)
	if err != nil || binding == "" {
		return backend.RemoteExecutionView{}, errors.New("inspect the current target before reading model settings")
	}
	execution, ok := client.(nativeProductExecution)
	if !ok {
		return backend.RemoteExecutionView{}, errors.New("target model settings are unavailable")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	caps, err := managed.ManagementCapabilities(ctx)
	if err != nil || !caps.Execution {
		return backend.RemoteExecutionView{}, errors.New("target model settings are unavailable")
	}
	view, err := execution.ExecutionSettings(ctx)
	if err != nil || view.Scope != scope || !productmanagement.ValidExecutionView(view) || !e.managementCurrent(binding, client) {
		return backend.RemoteExecutionView{}, errors.New("target model settings changed or are unavailable")
	}
	return backend.RemoteExecutionView{Binding: binding, ConversationDefault: view.ConversationDefault, Conversation: view.Conversation, Work: view.Work, Revision: view.Revision, Models: view.Models}, nil
}

func (e *productEngine) ChangeRemoteExecutionSettings(parent context.Context, request backend.RemoteExecutionRequest) (backend.RemoteManagementResult, error) {
	e.op.Lock()
	defer e.op.Unlock()
	client, managed, scope, err := e.managementClient(request.Binding)
	if err != nil || request.Binding == "" {
		return backend.RemoteManagementResult{}, errors.New("target model connection changed; inspect before applying changes")
	}
	execution, ok := client.(nativeProductExecution)
	if !ok {
		return backend.RemoteManagementResult{}, errors.New("target model settings are unavailable")
	}
	ctx, done := e.managementContext(parent)
	defer done()
	caps, err := managed.ManagementCapabilities(ctx)
	if err != nil || !caps.Execution {
		return backend.RemoteManagementResult{}, errors.New("target model settings are unavailable")
	}
	command := productmanagement.ExecutionCommand{Scope: scope, ID: request.ID, Target: request.Target, ExpectedRevision: request.ExpectedRevision, Selection: request.Selection}
	if !productmanagement.ValidExecutionCommand(command) {
		return backend.RemoteManagementResult{}, errors.New("invalid target model selection")
	}
	if err = e.reserveManagement(request.ID, "configure-execution", scope, command, nil); err != nil {
		return backend.RemoteManagementResult{}, err
	}
	if !e.managementCurrent(request.Binding, client) {
		return backend.RemoteManagementResult{ID: request.ID, Outcome: "unknown"}, errors.New("target connection ended before a model receipt was observed")
	}
	result, err := execution.ChangeExecutionSettings(ctx, command)
	if result.Scope != scope || !e.managementCurrent(request.Binding, client) {
		err = errors.New("target model receipt connection changed")
	}
	return e.finishManagement(request.ID, backend.RemoteManagementResult{ID: result.ID, Outcome: result.Outcome, Code: result.Code}, err)
}
