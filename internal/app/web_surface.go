package app

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// SurfaceTasks exposes the existing task ledger independently of native
// terminal-window support. It creates no task or execution owner.
func (a *Application) SurfaceTasks() []api.Task {
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return []api.Task{}
	}
	return m.ListTasks()
}

func (a *Application) SurfaceTaskConversation(ctx context.Context, id string) (api.TaskConversation, error) {
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return api.TaskConversation{}, errors.New("task connection unavailable")
	}
	return m.ReadTaskConversation(ctx, id)
}
