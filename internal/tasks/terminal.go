package tasks

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"sort"
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
		if r.Provider != m.provider || r.View.Outcome == "rejected" || (current && state.Task.Outcome == "rejected") {
			continue
		}
		prompt := r.OriginalPrompt
		if prompt == "" {
			prompt = state.OriginalPrompt
		}
		out = append(out, api.TaskPreview{ID: id, Prompt: prompt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	m.op.Lock()
	defer m.op.Unlock()
	if m.paused {
		return api.TerminalTarget{}, errors.New("正在更新，请稍后打开任务")
	}
	if err := m.owned(id); err != nil {
		return api.TerminalTarget{}, err
	}
	p, ok := m.work.(api.WorkTerminalProvider)
	if !ok {
		return api.TerminalTarget{}, errors.New("当前运行时暂不支持终端观察")
	}
	return p.WorkTerminal(ctx, id)
}
