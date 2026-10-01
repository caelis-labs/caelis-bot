package app

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func (a *Application) TaskPreferences() tasks.Preferences {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.TaskPreferences()
	}
	if NodeRoamingOwnsExecution(a) {
		return tasks.Preferences{MaxRunning: 1, Terminal: "system"}
	}
	if a.taskPreferences == nil {
		return tasks.Preferences{MaxRunning: 1, Terminal: "system"}
	}
	return a.taskPreferences.Snapshot()
}
func (a *Application) SaveTaskPreferences(p tasks.Preferences) (tasks.Preferences, error) {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.SaveTaskPreferences(p)
	}
	if NodeRoamingOwnsExecution(a) {
		return p, errors.New("remote Bot task preferences are managed on its host")
	}
	if a.taskPreferences == nil {
		return p, errors.New("remote Bot task preferences are managed on its host")
	}
	return a.taskPreferences.Save(p)
}
func (a *Application) PinTask(id string, pin bool) (api.TaskSummary, error) {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.PinTask(id, pin)
	}
	if NodeRoamingOwnsExecution(a) {
		return api.TaskSummary{}, errors.New("native local operation unavailable during roaming")
	}
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return api.TaskSummary{}, errors.New(a.text("taskNotConnected"))
	}
	return m.PinTask(id, pin)
}

func (a *Application) LockTask(id string, locked bool) (api.TaskSummary, error) {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.LockTask(id, locked)
	}
	if NodeRoamingOwnsExecution(a) {
		return api.TaskSummary{}, errors.New("native local operation unavailable during roaming")
	}
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return api.TaskSummary{}, errors.New(a.text("taskNotConnected"))
	}
	return m.LockTask(id, locked)
}
func (a *Application) ClearTasks() error {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.ClearTasks()
	}
	if NodeRoamingOwnsExecution(a) {
		return errors.New("native local operation unavailable during roaming")
	}
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return errors.New(a.text("taskNotConnected"))
	}
	return m.ClearTasks()
}

func (a *Application) MoveTask(id, before string) error {
	if active := ActiveNodeRoamingApplication(a); active != a {
		return active.MoveTask(id, before)
	}
	if NodeRoamingOwnsExecution(a) {
		return errors.New("native local operation unavailable during roaming")
	}
	a.mu.Lock()
	m, closed := a.tasks, a.closed
	a.mu.Unlock()
	if m == nil || closed {
		return errors.New(a.text("taskNotConnected"))
	}
	return m.MoveTask(id, before)
}
