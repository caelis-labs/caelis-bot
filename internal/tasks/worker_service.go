package tasks

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/workerfiles"
	"path/filepath"
	"slices"
)

func (m *Manager) stopWork(ctx context.Context, id string) (api.Task, error) {
	p, ok := m.work.(api.WorkStopper)
	if !ok {
		return m.work.StopWork(ctx, id)
	}
	key := ""
	for _, state := range m.work.WorkStates() {
		if state.Task.ID == id {
			key = state.StopExecutionKey
			if key == "" {
				key = state.ExecutionKey
			}
			break
		}
	}
	// Stable for this execution, including a lost response. A subsequent turn has
	// a different native key; a late old request cannot stop the later turn.
	op := "stop-" + api.WorkFingerprint(struct{ ID, Execution string }{id, key})
	in := api.WorkStop{ID: id, RequestID: op, ExecutionKey: key}
	m.mu.Lock()
	r := m.state.Records[id]
	if r.Operations == nil {
		r.Operations = map[string]api.WorkOperationQuery{}
	}
	r.Operations[op] = api.WorkOperationQuery{TaskID: id, RequestID: op, Kind: "stop", Fingerprint: api.WorkFingerprint(in)}
	err := m.write()
	m.mu.Unlock()
	if err != nil {
		return api.Task{}, err
	}
	return p.StopWorkRequest(ctx, in)
}
func (m *Manager) MaterializeTaskFile(ctx context.Context, id, ref string, refresh bool) (api.TaskFileCopy, error) {
	if err := m.owned(id); err != nil {
		return api.TaskFileCopy{}, err
	}
	if parsed, ok := api.ParseTaskArtifactID(ref); ok {
		if parsed.TaskID != id {
			return api.TaskFileCopy{}, errors.New("worker_file_source_conflict")
		}
		ref = parsed.ID
	}
	m.fileCopiesOnce.Do(func() { m.fileCopies = workerfiles.OpenOutputs(filepath.Join(m.root, ".files")) })
	return m.fileCopies.Materialize(ctx, id, ref, refresh, func(ctx context.Context) (api.WorkFileContent, error) { return m.ReadTaskFile(ctx, id, ref) })
}
func (m *Manager) ObserveTask(ctx context.Context, id string) (api.WorkObservation, error) {
	if err := m.owned(id); err != nil {
		return api.WorkObservation{}, err
	}
	p, ok := m.work.(api.WorkObserver)
	if !ok {
		return api.WorkObservation{}, errors.New("worker_observe_unsupported")
	}
	v, err := p.ObserveWork(ctx, id)
	v.Items = slices.Clone(v.Items)
	for i := range v.Items {
		v.Items[i].Artifacts = slices.Clone(v.Items[i].Artifacts)
		for j := range v.Items[i].Artifacts {
			a := &v.Items[i].Artifacts[j]
			a.ID = api.TaskArtifactID(id, a.ID)
		}
	}
	return v, err
}
func (m *Manager) ReadTaskFile(ctx context.Context, id, ref string) (api.WorkFileContent, error) {
	if err := m.owned(id); err != nil {
		return api.WorkFileContent{}, err
	}
	p, ok := m.work.(api.WorkFileReader)
	if !ok {
		return api.WorkFileContent{}, errors.New("worker_files_unsupported")
	}
	if parsed, ok := api.ParseTaskArtifactID(ref); ok {
		if parsed.TaskID != id {
			return api.WorkFileContent{}, errors.New("worker_file_source_conflict")
		}
		ref = parsed.ID
	}
	return p.ReadWorkFile(ctx, id, ref)
}
func (m *Manager) ReconcileTaskOperation(ctx context.Context, in api.WorkOperationQuery) (api.WorkOperation, error) {
	if err := m.owned(in.TaskID); err != nil {
		return api.WorkOperation{}, err
	}
	p, ok := m.work.(api.WorkOperationReader)
	if !ok {
		return api.WorkOperation{}, errors.New("worker_reconcile_unsupported")
	}
	return p.ReadWorkOperation(ctx, in)
}

func (m *Manager) ReconcileTaskRequest(ctx context.Context, id, request string) (api.WorkOperation, error) {
	if err := m.owned(id); err != nil {
		return api.WorkOperation{}, err
	}
	m.mu.Lock()
	q, ok := m.state.Records[id].Operations[request]
	m.mu.Unlock()
	if !ok {
		return api.WorkOperation{}, errors.New("original_operation_digest_unavailable")
	}
	return m.ReconcileTaskOperation(ctx, q)
}
