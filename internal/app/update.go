package app

import "errors"

// PrepareUpdate freezes new user, task and scheduled admission. Native owners
// retain unresolved work and original receipts across the update handoff; an
// explicit install request must not wait for an approval or Worker to finish.
// The caller must Close or CancelUpdate on success.
func (a *Application) PrepareUpdate() error {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	a.mu.Lock()
	resident, tasks, closed, prepared := a.companion, a.tasks, a.closed, a.updatePrepared
	a.mu.Unlock()
	if closed {
		return errors.New(a.text("host.appStopped"))
	}
	if prepared {
		return errors.New("正在重新启动")
	}
	guard := func() error {
		a.mu.Lock()
		closed := a.closed
		a.mu.Unlock()
		if closed {
			return errors.New(a.text("host.appStopped"))
		}
		if err := a.Backend.CanDetachForUpdate(); err != nil {
			return err
		}
		if a.localWork != nil {
			return a.localWork.CanDetachForUpdate()
		}
		return nil
	}
	pauseTasks := func() error {
		if tasks != nil {
			// The update-specific task fence persists original owner and
			// receipts before allowing an active task to outlive this app.
			if p, ok := any(tasks).(interface{ PauseForUpdate(func() error) error }); ok {
				return p.PauseForUpdate(guard)
			}
			// Older task managers retain the previous safe idle-only gate.
			return tasks.PauseIfIdle(guard)
		}
		return guard()
	}
	// Do not hold Backend admission while waiting for a running scheduled Tick:
	// Tick can itself be waiting for that admission lock to submit.
	var err error
	if resident != nil {
		err = resident.PauseIfIdle(pauseTasks)
	} else {
		err = pauseTasks()
	}
	if err != nil {
		return err
	}
	// Any user input that raced with the pause has now drained through this
	// admission lock. Recheck detach readiness against its latest native state.
	if err = a.Backend.PrepareRestart(guard); err != nil {
		if tasks != nil {
			tasks.ResumeAfterUpdate()
		}
		if resident != nil {
			resident.ResumeAfterUpdate()
		}
		return err
	}
	a.mu.Lock()
	a.updatePrepared = true
	a.mu.Unlock()
	return nil
}

func (a *Application) CancelUpdate() {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	a.mu.Lock()
	resident, tasks := a.companion, a.tasks
	a.updatePrepared = false
	a.mu.Unlock()
	if tasks != nil {
		tasks.ResumeAfterUpdate()
	}
	if resident != nil {
		resident.ResumeAfterUpdate()
	}
	a.Backend.CancelRestart()
}
