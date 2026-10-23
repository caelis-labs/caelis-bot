package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (w *localWorkers) ValidateWorkFiles(ctx context.Context, id string, files []api.InputFile) error {
	owner, err := w.target(ctx, id)
	if err != nil {
		return err
	}
	if p, ok := owner.(api.WorkInputValidator); ok {
		return p.ValidateWorkFiles(ctx, id, files)
	}
	return errors.New("worker_input_validation_unsupported")
}

func (w *localWorkers) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.WorkObservation{}, err
	}
	p, ok := owner.(api.WorkObserver)
	if !ok {
		return api.WorkObservation{}, errors.New("worker_observe_unsupported")
	}
	v, err := p.ObserveWork(ctx, id)
	w.mu.Lock()
	v.State.Runtime = w.routes[id]
	w.mu.Unlock()
	return v, err
}
func (w *localWorkers) WorkCapabilities(ctx context.Context, id string) (api.WorkCapabilities, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.WorkCapabilities{}, err
	}
	p, ok := owner.(api.WorkCapabilityProvider)
	if !ok {
		return api.WorkCapabilities{}, errors.New("worker_contract_unsupported")
	}
	return p.WorkCapabilities(ctx, id)
}
func (w *localWorkers) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	w.mu.Lock()
	runtime, active := w.routes[in.ID], w.active
	owner := api.WorkRuntime(w.owners[runtime])
	if runtime == active {
		owner = w.resident
	}
	w.mu.Unlock()
	if owner == nil {
		var err error
		owner, err = w.target(ctx, in.ID)
		if err != nil {
			return api.Task{}, err
		}
	}
	if p, ok := owner.(api.WorkStopper); ok {
		return p.StopWorkRequest(ctx, in)
	}
	return owner.StopWork(ctx, in.ID) // only pre-contract local/fixture owners
}
func (w *localWorkers) DecideWork(ctx context.Context, in api.WorkDecision) error {
	owner, err := w.target(ctx, in.TaskID)
	if err != nil {
		return err
	}
	p, ok := owner.(api.WorkDecider)
	if !ok {
		return errors.New("worker_interactions_unsupported")
	}
	return p.DecideWork(ctx, in)
}
func (w *localWorkers) WorkApprovalURL(ctx context.Context, id, request string) (string, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return "", err
	}
	p, ok := owner.(api.WorkDecider)
	if !ok {
		return "", errors.New("worker_url_unsupported")
	}
	return p.WorkApprovalURL(ctx, id, request)
}
func (w *localWorkers) ReadWorkOperation(ctx context.Context, in api.WorkOperationQuery) (api.WorkOperation, error) {
	owner, err := w.target(ctx, in.TaskID)
	if err != nil {
		return api.WorkOperation{}, err
	}
	p, ok := owner.(api.WorkOperationReader)
	if !ok {
		return api.WorkOperation{}, api.ErrWorkOperationUnsupported
	}
	return p.ReadWorkOperation(ctx, in)
}
func (w *localWorkers) ReadWorkFile(ctx context.Context, id, ref string) (api.WorkFileContent, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.WorkFileContent{}, err
	}
	p, ok := owner.(api.WorkFileReader)
	if !ok {
		return api.WorkFileContent{}, errors.New("worker_files_unsupported")
	}
	return p.ReadWorkFile(ctx, id, ref)
}

func (a *Application) workerStates() []api.WorkState {
	if a.machines != nil {
		return a.machines.WorkStates()
	}
	if a.localWork != nil {
		return a.localWork.WorkStates()
	}
	return nil
}
func (a *Application) retainedWorkerApprovals() []api.Approval {
	if a.machines != nil {
		v, _ := a.machines.RetainedApprovalState()
		return v
	}
	if a.localWork != nil {
		return a.localWork.RetainedApprovals()
	}
	return nil
}
