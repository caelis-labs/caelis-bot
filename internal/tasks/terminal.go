package tasks

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"slices"
	"sort"
)

// TaskPreviews reads the product ledger; it never polls a Worker or reads a
// transcript. Stable handle ordering also survives process restarts.
func (m *Manager) TaskPreviews() []api.TaskPreview {
	// Read the adapter's in-memory projection, not its transcript. Native events
	// can precede the coordinator's final write of a start receipt.
	states, _ := m.workStates()
	latest := make(map[string]api.WorkState, len(states))
	for _, state := range states {
		latest[state.Task.ID] = state
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []api.TaskPreview{}
	for id, r := range m.state.Records {
		state, current := latest[id]
		current = current && state.Target == r.Target && state.Task.Workspace == r.View.Workspace
		newRun := current && ((r.Execution != "" && state.ExecutionKey != "" && state.ExecutionKey != r.Execution) || (terminal(r.View.Status) && !terminal(state.Task.Status)))
		if r.Provider != m.provider || (!newRun && (r.Pinned == nil || !*r.Pinned)) {
			continue
		}
		prompt := r.OriginalPrompt
		if prompt == "" {
			prompt = state.OriginalPrompt
		}
		status := r.View.Status
		if current {
			status = state.Task.Status
		}
		out = append(out, api.TaskPreview{ID: id, Prompt: prompt, Status: status, Provider: r.Provider, Locked: r.Locked})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		order := m.state.WatchOrder[m.provider]
		if len(order) > 0 {
			ai, bi := slices.Index(order, a.ID), slices.Index(order, b.ID)
			// New tasks not yet in the manual order appear first.
			if ai != bi {
				return ai < bi
			}
		}
		if terminal(a.Status) != terminal(b.Status) {
			return !terminal(a.Status)
		}
		ra, rb := m.state.Records[a.ID], m.state.Records[b.ID]
		if ra.ActiveAt != rb.ActiveAt {
			return ra.ActiveAt > rb.ActiveAt
		}
		return ra.Sequence > rb.Sequence
	})
	return out
}

func (m *Manager) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return api.TerminalTarget{}, errors.New(m.text("host.updatingOpenTaskLater"))
	}
	if err := m.owned(id); err != nil {
		return api.TerminalTarget{}, err
	}
	work, err := m.recordRuntime(id)
	if err != nil {
		return api.TerminalTarget{}, err
	}
	p, ok := work.(api.WorkTerminalProvider)
	if !ok {
		return api.TerminalTarget{}, errors.New(m.text("host.runtimeNoTerminalObservation"))
	}
	return p.WorkTerminal(ctx, id)
}
