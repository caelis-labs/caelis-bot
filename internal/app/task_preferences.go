package app

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func (a *Application) TaskPreferences() tasks.Preferences { return a.taskPreferences.Snapshot() }
func (a *Application) SaveTaskPreferences(p tasks.Preferences) (tasks.Preferences, error) {
	return a.taskPreferences.Save(p)
}
func (a *Application) PinTask(id string, pin bool) (api.TaskSummary, error) {
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return api.TaskSummary{}, errors.New(a.text("taskNotConnected"))
	}
	return m.PinTask(id, pin)
}
