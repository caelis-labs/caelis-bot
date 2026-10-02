package app

import (
	"context"
	"errors"
)

// retireNotebookSource closes local writers only after the actual live source is fenced.
func (a *Application) retireNotebookSource(ctx context.Context) error {
	a.mu.Lock()
	a.sourceRetired = true
	if a.notebookSyncCancel != nil {
		a.notebookSyncCancel()
	}
	a.started = false
	if a.cancel != nil {
		a.cancel()
	}
	resident, bridge := a.companion, a.bridge
	if resident != nil {
		resident.Stop()
	}
	a.mu.Unlock()
	var err error
	if a.workerNodes != nil {
		err = errors.Join(err, a.workerNodes.Close())
	}
	err = errors.Join(err, a.engine.Close(ctx))
	if resident != nil {
		resident.Close()
	}
	if bridge != nil {
		bridge.Close()
	}
	a.workers.Wait()
	if a.notebook != nil {
		err = errors.Join(err, a.notebook.Close())
	}
	if a.personal != nil {
		err = errors.Join(err, a.personal.Close())
	}
	a.mu.Lock()
	a.companion, a.bridge, a.tasks, a.notebook, a.personal = nil, nil, nil, nil, nil
	a.cancel = nil
	a.mu.Unlock()
	return err
}
