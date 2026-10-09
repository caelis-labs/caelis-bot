package app

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

func (a *Application) queuePluginIndex() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.workers.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.workers.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.syncPluginIndex(ctx); err != nil && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}()
}

// Package and credential writes confirm first. Runtime assembly follows on a
// separate worker and never changes the original management result.
func (a *Application) queuePluginReconcile() {
	if a.plugins == nil {
		return
	}
	a.mu.Lock()
	if !a.started || a.closed {
		a.mu.Unlock()
		return // Start reads the latest confirmed selection before connecting.
	}
	a.pluginSyncMu.Lock()
	a.pluginSyncPending = true
	a.pluginSyncError = false
	if a.pluginSyncRunning {
		a.pluginSyncMu.Unlock()
		a.mu.Unlock()
		return
	}
	a.pluginSyncRunning = true
	a.workers.Add(1)
	a.pluginSyncMu.Unlock()
	a.mu.Unlock()
	go a.reconcilePlugins()
}

func (a *Application) reconcilePlugins() {
	defer a.workers.Done()
	for {
		a.pluginSyncMu.Lock()
		if !a.pluginSyncPending {
			a.pluginSyncRunning = false
			a.pluginSyncMu.Unlock()
			return
		}
		a.pluginSyncPending = false
		a.pluginSyncMu.Unlock()

		selection := a.plugins.Selection()
		a.pluginSyncMu.Lock()
		alreadyProjected := selection.Revision == a.pluginSyncRevision
		a.pluginSyncMu.Unlock()
		if alreadyProjected {
			continue
		}
		adapter, ok := a.engine.(api.PluginConfigurator)
		if !ok {
			a.pluginSyncMu.Lock()
			a.pluginSyncError = true
			a.pluginSyncRunning = false
			a.pluginSyncMu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		a.pluginAdmission.Lock()
		a.mu.Lock()
		closed := a.closed
		a.mu.Unlock()
		var err error
		projectedRevision := uint64(0)
		if !closed {
			err = adapter.WithBotPluginAdmission(ctx, func(apply func(context.Context, plugins.Selection) error) error {
				// Actions may have changed while waiting for the Runtime lock.
				// Publish the newest confirmed selection once that lock is ours.
				latest := a.plugins.Selection()
				projectedRevision = latest.Revision
				return apply(ctx, latest)
			})
		}
		a.pluginAdmission.Unlock()
		cancel()
		if closed {
			a.pluginSyncMu.Lock()
			a.pluginSyncRunning = false
			a.pluginSyncMu.Unlock()
			return
		}
		if projectedRevision == 0 && errors.Is(err, context.DeadlineExceeded) {
			// The Runtime lock was never acquired, so no native update was
			// dispatched. Wait for a free turn and retry the latest selection.
			a.pluginSyncMu.Lock()
			a.pluginSyncPending = true
			a.pluginSyncMu.Unlock()
			time.Sleep(2 * time.Second)
			continue
		}
		if err == nil && projectedRevision == 0 {
			err = errors.New("plugin Runtime admission returned without applying a selection")
		}
		if err != nil {
			// An unknown native result retains its original receipt in the
			// adapter. No automatic redispatch follows this error.
			a.pluginSyncMu.Lock()
			a.pluginSyncError = true
			a.pluginSyncRunning = false
			a.pluginSyncMu.Unlock()
			return
		}
		latestRevision := a.plugins.Selection().Revision
		a.pluginSyncMu.Lock()
		a.pluginSyncRevision = projectedRevision
		a.pluginSyncError = false
		if latestRevision != projectedRevision {
			a.pluginSyncPending = true
		}
		a.pluginSyncMu.Unlock()
		a.pluginDetailMu.Lock()
		a.pluginDetailCache = nil
		a.pluginDetailMu.Unlock()
		indexCtx, indexCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.syncPluginIndex(indexCtx); err != nil && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
		indexCancel()
	}
}
