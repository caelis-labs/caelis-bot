package app

import "errors"

var ErrManagedChannelHandoff = errors.New("channel handoff pending")

// PrepareIdleUpdate is intentionally stricter than the native explicit-update
// detach path. The first managed Linux release never authorizes active work to
// outlive a supervisor replacement. Failed preflight leaves admission open.
func (a *Application) PrepareIdleUpdate() error {
	if a.setup != nil {
		if !a.setup.mu.TryLock() {
			return errors.New("runtime operation pending")
		}
		defer a.setup.mu.Unlock()
		if p := a.setup.InstallProgress(); p.State == "running" || p.State == "unknown" {
			return errors.New("runtime installation pending")
		}
	}
	return a.prepareUpdate(true)
}

func (a *Application) guardManagedUpdate() error {
	// Do not call guardRuntimeChange here: task admission already holds its
	// owner lock, and the scheduler holds its step lock.
	if err := a.guardConversationChange(); err != nil {
		return err
	}
	// PrepareRestart holds backend admission; decorated Snapshot re-enters it.
	v := a.currentEngine().Snapshot()
	if v.Phase == "working" || v.Phase == "starting" {
		return errors.New("work pending")
	}
	if v.LoginPending {
		return errors.New("authorization pending")
	}
	for _, approval := range v.Approvals {
		if approval.Status != "resolved" {
			return errors.New("decision pending")
		}
	}
	for _, item := range v.Items {
		for _, q := range item.QuestionCards {
			switch q.State {
			case "answered", "dismissed", "expired", "cancelled", "completed":
			default:
				return errors.New("question pending")
			}
		}
	}
	return nil
}

func (a *Application) updateInProgress() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.updatePrepared || a.closed
}
