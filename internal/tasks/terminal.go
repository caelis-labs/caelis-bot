package tasks

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// TaskPreviews reads the product ledger; it never polls a Worker or reads a
// transcript. Stable handle ordering also survives process restarts.
func (m *Manager) TaskPreviews() []api.TaskPreview {
	if _, ok := m.work.(api.WorkTerminalProvider); !ok {
		return nil
	}
	// Read the adapter's in-memory projection, not its transcript. Native events
	// can precede the coordinator's final write of a start receipt.
	states := m.work.WorkStates()
	latest := make(map[string]api.WorkState, len(states))
	for _, state := range states {
		latest[state.Task.ID] = state
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []api.TaskPreview{}
	for id, r := range m.state.Records {
		state, current := latest[id]
		current = current && state.Task.Machine == r.View.Machine &&
			(r.Runtime == "" || state.Runtime == "" || r.Runtime == state.Runtime) &&
			(state.ExecutionKey == r.Execution || state.ExecutionKey != "" && !slices.Contains(r.RetiredExecutions, state.ExecutionKey))
		newRun := current && ((r.Execution != "" && state.ExecutionKey != "" && state.ExecutionKey != r.Execution) || (terminal(r.View.Status) && !terminal(state.Task.Status)))
		if !m.owns(r) || (!newRun && (r.Pinned == nil || !*r.Pinned)) {
			continue
		}
		prompt := r.OriginalPrompt
		if prompt == "" && current {
			prompt = state.OriginalPrompt
		}
		status := r.View.Status
		if current {
			status = state.Task.Status
		}
		provider := r.Runtime
		if provider == "" {
			provider = r.Provider
		}
		label := r.View.MachineName
		if current {
			label = firstMachineLabel(state.Task.MachineName, label)
		}
		noticeGeneration := unknownNoticeKey(r, id)
		out = append(out, api.TaskPreview{ID: id, Prompt: prompt, Status: status, Provider: provider, Locked: r.Locked, TargetLabel: label, NoticeGeneration: noticeGeneration, NoticeClaimed: r.UnknownNotice == noticeGeneration})
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
	m.mu.Lock()
	status := m.state.Records[id].View.Status
	m.mu.Unlock()
	if status == "unavailable" || status == "unknown" {
		return api.TerminalTarget{}, errors.New("retired or unresolved task cannot be continued in a terminal")
	}
	p, ok := m.work.(api.WorkTerminalProvider)
	if !ok {
		return api.TerminalTarget{}, errors.New(m.text("host.runtimeNoTerminalObservation"))
	}
	return p.WorkTerminal(ctx, id)
}

func firstMachineLabel(current, retained string) string {
	if current != "" {
		return current
	}
	return retained
}
