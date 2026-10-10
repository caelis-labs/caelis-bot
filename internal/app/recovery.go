package app

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"path/filepath"
	"time"
)

// Local feature assembly is retried independently of native surfaces and the
// Runtime observer. Existing identities/ledgers are reopened, never replaced.
func (a *Application) StartBackground() {
	a.mu.Lock()
	if a.startupCancel != nil || a.closed {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.startupCancel = cancel
	a.workers.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.workers.Done()
		var nextConnect time.Time
		for ctx.Err() == nil {
			if a.Telegram != nil {
				a.Telegram.Start()
			}
			if a.Weixin != nil {
				a.Weixin.Start()
			}
			var err error
			if a.HasRuntimeChoice() {
				err = a.Start()
			} else {
				err = a.PreparePersonal()
			}
			if err != nil && a.host.ReportError != nil {
				a.host.ReportError(err)
			}
			if a.HasRuntimeChoice() && time.Now().After(nextConnect) {
				state := a.Backend.RecoveryState()
				if !state.Automatic && !state.InProgress && (state.Manual || a.engine.Snapshot().Connection == "offline") {
					nextConnect = time.Now().Add(30 * time.Second)
					work, stop := context.WithTimeout(ctx, 30*time.Second)
					_ = a.Backend.Connect(work)
					stop()
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}
func (a *Application) repairFeaturesLocked() error {
	if err := a.preparePersonalLocked(); err != nil {
		return err
	}
	if a.tasks == nil {
		manager, err := tasks.Open(filepath.Join(a.root, "tasks.json"), filepath.Join(a.root, "Tasks"), a.engine.(api.Provider).ProviderInfo().ID, a.machines, a.engine.(api.ReportSubmitter), a.engine.Snapshot)
		if err != nil {
			return err
		}
		manager.SetLocale(a.locale)
		manager.ConfigureLimit(func() int { return a.taskPreferences.Snapshot().MaxRunning })
		manager.ObserveWatchlist(a.host.ObserveTasks)
		if importer, ok := a.engine.(interface{ ImportHostReportIDs([]string) error }); ok {
			if err := importer.ImportHostReportIDs(manager.HostReportIDs()); err != nil {
				return err
			}
		}
		if err := a.companion.ConfigureTasks(manager, manager); err != nil {
			return err
		}
		a.tasks = manager
	}
	return nil
}
