package tasks

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// SetFinishedObserver connects the durable product boundary to temporary cards.
// The observer is local persistence only; it must not call back into Manager.
func (m *Manager) SetFinishedObserver(f func(api.WorkState) error) {
	m.mu.Lock()
	m.finished = f
	m.mu.Unlock()
}

// InteractionStates overlays only an explicit, generation-fenced finish receipt.
// Native turn completion, result prose and report delivery never imply finish.
func (m *Manager) InteractionStates(states []api.WorkState) []api.WorkState {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range states {
		r := m.state.Records[states[i].Task.ID]
		if r != nil && m.owns(r) && r.Runtime == states[i].Runtime && r.View.Machine == states[i].Task.Machine && r.FinishedExecution != "" && r.FinishedExecution == states[i].ExecutionKey {
			states[i].TaskEnded = true
		}
	}
	return states
}

func (m *Manager) FinishTask(ctx context.Context, id, token string) (api.Task, error) {
	release, err := m.environmentOperation(ctx, m.taskEnvironment(id))
	if err != nil {
		return api.Task{}, err
	}
	defer release()
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return api.Task{}, errors.New("task update in progress")
	}
	if err := m.owned(id); err != nil {
		return api.Task{}, err
	}
	// Fresh original-owner read. A cached completed card cannot authorize finish.
	if _, err := workerIO(m, m.taskEnvironment(id), func() (api.Task, error) { return m.work.ReadWork(ctx, id) }); err != nil {
		return api.Task{}, err
	}
	if m.paused {
		return api.Task{}, errors.New("task update in progress")
	}
	if err := m.refresh(); err != nil {
		return api.Task{}, err
	}
	var state api.WorkState
	for _, v := range m.work.WorkStates() {
		if v.Task.ID == id {
			state = v
			break
		}
	}
	if token == "" || token != api.TaskFinishToken(state) || state.ExecutionKey == "" || state.Activity != "idle" || !terminal(state.Task.Status) || state.StopPending {
		return api.Task{}, errors.New("original task must be confirmed idle with a terminal result before finish")
	}
	if p, ok := m.work.(interface {
		RetainedApprovalState() ([]api.Approval, uint64)
	}); ok {
		approvals, _ := p.RetainedApprovalState()
		for _, a := range approvals {
			if a.TaskID == id && a.Status == "pending" {
				return api.Task{}, errors.New("original task still has a pending native approval")
			}
		}
	}
	m.mu.Lock()
	r := m.state.Records[id]
	if r.Runtime != state.Runtime || r.View.Machine != state.Task.Machine || r.Execution != state.ExecutionKey {
		m.mu.Unlock()
		return api.Task{}, errors.New("original task generation changed")
	}
	previous := r.FinishedExecution
	r.FinishedExecution = state.ExecutionKey
	err = m.write()
	if err != nil {
		r.FinishedExecution = previous
	}
	observer := m.finished
	m.mu.Unlock()
	if err != nil {
		return api.Task{}, err
	}
	state.TaskEnded = true
	if observer != nil {
		// A failed card save keeps the finish receipt. Retrying repeats local cleanup,
		// never a native operation; the observer/next startup also reconciles it.
		if err := observer(state); err != nil {
			return api.Task{}, err
		}
	}
	return state.Task, nil
}

func (m *Manager) reconcileFinishedBeforeInput(id string) error {
	m.mu.Lock()
	observer := m.finished
	m.mu.Unlock()
	if observer == nil {
		return nil
	}
	for _, s := range m.InteractionStates(m.work.WorkStates()) {
		if s.Task.ID == id && s.TaskEnded {
			return observer(s)
		}
	}
	return nil
}
