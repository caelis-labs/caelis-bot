package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// activateRuntime replaces only this Bot's resident adapter. The native app,
// windows, shared Host, user CLI, and provider-owned Stores stay in place.
// The saved selection is committed only after the new adapter handshakes.
func (a *Application) activateRuntime(ctx context.Context, settings api.RuntimeSettings, first bool) error {
	if !first && a.Backend.RuntimeSettings() == settings {
		return a.Backend.Connect(ctx)
	}
	// Drain the inactive writer before loading the provider's latest ledger.
	if err := a.localWork.BeginResidentSwitch(ctx, settings.Runtime); err != nil {
		return err
	}
	defer a.localWork.EndResidentSwitch()
	factory, err := a.resolve(settings.Runtime)
	if err != nil {
		return err
	}
	if factory.ID != settings.Runtime || factory.Open == nil {
		return errors.New(a.text("backendFactoryMismatch", nil))
	}
	directory, err := providerDirectory(a.root, settings.Runtime)
	if err != nil {
		return err
	}
	old := a.Backend.CurrentRuntimeBinding()
	prepared, err := backend.PrepareRuntimeBinding(a.root, settings, factory.Defaults, nil)
	if err != nil {
		return err
	}
	// Before a first connection the composer belongs to the Bot shell. Carry
	// unsent input into the selected provider only if that provider has no draft.
	transferDraft := first && prepared.Draft.Text == "" && prepared.Draft.Quoted == nil && len(prepared.Draft.ReferenceIDs) == 0 &&
		(old.Draft.Text != "" || old.Draft.Quoted != nil || len(old.Draft.ReferenceIDs) != 0)
	if transferDraft {
		prepared.Draft = old.Draft
	}
	engine, err := factory.Open(providerConfig{
		Diagnostics: a.host.Diagnostics, Settings: settings, Execution: prepared.Execution,
		WorkExecution: prepared.Work, WorkDirectory: filepath.Join(directory, "Work"),
		WorkRoot: filepath.Join(a.root, "Tasks"), ConversationFile: filepath.Join(directory, "conversation.json"),
	})
	if err != nil {
		return err
	}
	if err = requireAssistant(engine, settings.Runtime, a.locale()); err != nil {
		_ = engine.Close(context.Background())
		return err
	}
	prepared.Engine = engine
	committed := false
	defer func() {
		if !committed {
			closeRuntimeEngine(engine)
		}
	}()

	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	a.pluginAdmission.Lock()
	defer a.pluginAdmission.Unlock()
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	a.mu.Lock()
	resident, manager, closed, started := a.companion, a.tasks, a.closed, a.started
	a.mu.Unlock()
	if closed {
		return errors.New(a.text("appStopped"))
	}
	guard := func() error { return a.guardConversationChange() }
	if manager != nil {
		if err = manager.PauseForUpdate(guard); err != nil {
			return err
		}
	}
	if resident != nil && started {
		if err = resident.PauseIfIdle(guard); err != nil {
			if manager != nil {
				manager.ResumeAfterUpdate()
			}
			return err
		}
	}
	if err = a.Backend.BeginRuntimeSwitch(guard); err != nil {
		if resident != nil && started {
			resident.ResumeAfterUpdate()
		}
		if manager != nil {
			manager.ResumeAfterUpdate()
		}
		return err
	}
	defer a.Backend.EndRuntimeSwitch()

	oldDirectory, _ := providerDirectory(a.root, old.Settings.Runtime)
	a.stopResidentRuntime()
	// Keep one writer for the outgoing Workers during preparation/rollback.
	a.localWork.RetainResident(old.Settings.Runtime, old.Engine)
	apply := func(binding backend.RuntimeBinding, directory string) error {
		if a.host.RuntimeChanged != nil {
			if e := a.host.RuntimeChanged(directory, first); e != nil {
				return e
			}
		}
		a.Backend.BindRuntime(binding)
		a.setEngine(binding.Engine)
		a.localWork.SwitchResident(binding.Settings.Runtime, binding.Engine)
		return a.startRuntime()
	}
	if err = apply(prepared, directory); err == nil {
		if transferDraft {
			_, err = a.Backend.SaveDraft(prepared.Draft)
		}
	}
	if err == nil {
		err = a.Backend.ConnectPreparedRuntime(ctx)
	}
	if err == nil && engine.Snapshot().Connection != "ready" {
		err = errors.New("连接尚未就绪，请重新检测并重试")
	}
	if err == nil {
		err = ctx.Err()
	}
	oldWorkerDefault := a.localWork.DefaultRuntime()
	workerDefaultChanged := false
	if err == nil {
		// Keep the old profile for later selection; no Runtime is duplicated or
		// migrated. Existing original task routes remain in their own namespace.
		if !first && old.Settings.Runtime != settings.Runtime {
			err = backend.SaveRuntimeProfile(a.root, old.Settings)
		}
		if err == nil {
			err = backend.SaveRuntimeProfile(a.root, settings)
		}
		if err == nil {
			if first {
				err = a.localWork.SelectFirstRuntime("conversation")
				workerDefaultChanged = err == nil
			}
		}
		if err == nil {
			err = backend.SaveActiveRuntimeSettings(a.root, settings)
		}
	}
	if err == nil {
		committed = true
		a.Backend.RequireSetup(false)
		if old.Settings.Runtime == settings.Runtime {
			// First activation replaces an unconnected shell in the same namespace.
			_ = detachRuntimeObserver(context.Background(), old.Engine)
		} else if err := a.localWork.RetireResident(ctx, old.Settings.Runtime); err != nil {
			// Leave the live owner routable if observer handoff needs recovery.
			a.host.Diagnostics.Write(diagnosticlog.Record{Component: "runtime_switch", Code: "worker_owner_retained"})
		} else if _, ok := old.Engine.(retainedWorker); !ok {
			closeRuntimeEngine(old.Engine)
		}
		return nil
	}

	// No durable active selection was changed on handshake failure. Restore
	// the original resident Bot from its same identity and receipt ledgers.
	if workerDefaultChanged {
		err = errors.Join(err, a.localWork.SelectFirstRuntime(oldWorkerDefault))
	}
	a.stopResidentRuntime()
	a.localWork.RetainResident(settings.Runtime, engine)
	committed = true // the pool owns recovery of a failed target detach
	var hostRestore error
	if a.host.RuntimeChanged != nil {
		hostRestore = a.host.RuntimeChanged(oldDirectory, false)
	}
	draftRestore := a.Backend.RestoreRuntimeBinding(old, prepared.Draft.Revision)
	a.setEngine(old.Engine)
	a.localWork.SwitchResident(old.Settings.Runtime, old.Engine)
	if settings.Runtime != old.Settings.Runtime {
		if detachErr := a.localWork.RetireResident(context.Background(), settings.Runtime); detachErr != nil {
			err = errors.Join(err, detachErr)
		} else if _, ok := engine.(retainedWorker); !ok {
			closeRuntimeEngine(engine)
		}
	} else {
		err = errors.Join(err, detachRuntimeObserver(context.Background(), engine))
	}
	if rebind, ok := old.Engine.(interface{ PrepareRuntimeRebind(context.Context) error }); ok {
		rebindCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err = errors.Join(err, rebind.PrepareRuntimeRebind(rebindCtx))
		cancel()
	}
	startRestore := a.startRuntime()
	var connectRestore error
	if startRestore == nil && !first {
		// The old client was paused but not closed. Reconnect only its Bot
		// observer; never stop the shared Runtime Host. Setup cancellation
		// cannot cancel restoration of the original client.
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 30*time.Second)
		connectRestore = a.Backend.ConnectPreparedRuntime(restoreCtx)
		restoreCancel()
	}
	if hostRestore != nil || startRestore != nil || connectRestore != nil {
		return errors.Join(err, errors.New("原连接恢复未完成"), hostRestore, startRestore, connectRestore, draftRestore)
	}
	return errors.Join(err, draftRestore)
}

func (a *Application) stopResidentRuntime() {
	a.mu.Lock()
	cancel, resident, bridge := a.cancel, a.companion, a.bridge
	a.cancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if resident != nil {
		resident.Close()
	}
	a.runtimeWorkers.Wait()
	if bridge != nil {
		bridge.Close()
	}
	if a.plugins != nil {
		a.plugins.CloseDesktopOwner()
	}
	a.mu.Lock()
	a.companion, a.bridge, a.tasks, a.started = nil, nil, nil, false
	a.nativeSourcesReady, a.dreamReady = false, false
	a.mu.Unlock()
	a.Backend.SetBotStatus(nil)
	a.Backend.SetUserSubmitter(nil)
}

func closeRuntimeEngine(engine api.Engine) {
	if engine == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = engine.Close(ctx)
}
