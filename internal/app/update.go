package app

// PrepareUpdate freezes both user admission and scheduled admission before the
// native updater closes the application. Busy/uncertain work is never cancelled
// merely to install an update. The caller must Close or CancelUpdate on success.
func (a *Application) PrepareUpdate() error {
	return a.Backend.PrepareRestart(func() error {
		a.mu.Lock()
		resident, tasks := a.companion, a.tasks
		a.mu.Unlock()
		guard := func() error {
			if err := a.guardConversationChange(); err != nil {
				return err
			}
			return a.guardReminderChange()
		}
		pauseTasks := func() error {
			if tasks != nil {
				return tasks.PauseIfIdle(guard)
			}
			return guard()
		}
		if resident != nil {
			return resident.PauseIfIdle(pauseTasks)
		}
		return pauseTasks()
	})
}

func (a *Application) CancelUpdate() {
	a.mu.Lock()
	resident, tasks := a.companion, a.tasks
	a.mu.Unlock()
	if tasks != nil {
		tasks.ResumeAfterUpdate()
	}
	if resident != nil {
		resident.ResumeAfterUpdate()
	}
	a.Backend.CancelRestart()
}
