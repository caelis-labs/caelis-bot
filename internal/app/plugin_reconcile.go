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

func (a *Application) observePluginReadiness(connection string, priorConnection *string, priorRecoveryPending *bool) {
	pending := false
	if recovery, ok := a.engine.(interface{ BotPluginRecoveryPending() bool }); ok {
		pending = recovery.BotPluginRecoveryPending()
	}
	if connection == "ready" && (*priorConnection != "ready" || *priorRecoveryPending && !pending) {
		// Ready owners can resolve the original receipt without disconnecting.
		// The adapter retains the unknown-operation fence until readback ends.
		a.queuePluginReconcile()
	}
	*priorConnection, *priorRecoveryPending = connection, pending
}

// A new management or recovery signal may arrive while the previous Runtime
// attempt is finishing. Consume only that queued signal; an unresolved native
// receipt is still fenced by the adapter and is never redispatched here.
func (a *Application) finishFailedPluginReconcile() bool {
	a.pluginSyncMu.Lock()
	defer a.pluginSyncMu.Unlock()
	a.pluginSyncError = true
	if a.pluginSyncPending {
		return true
	}
	a.pluginSyncRunning = false
	return false
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
			if a.finishFailedPluginReconcile() {
				continue
			}
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
			if a.finishFailedPluginReconcile() {
				continue
			}
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
		if cleanupErr := a.plugins.ConfirmOAuthProjection(projectedRevision); cleanupErr != nil && a.host.ReportError != nil {
			a.host.ReportError(cleanupErr)
		}
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
