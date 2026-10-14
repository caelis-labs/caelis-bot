package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// activateRuntime replaces only this Bot's resident adapter. The native app,
// windows, shared Host, user CLI, and provider-owned Stores stay in place.
// The saved selection is committed only after the new adapter handshakes.
func (a *Application) activateRuntime(ctx context.Context, settings api.RuntimeSettings, first bool) error {
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
	if err = a.localWork.CanSwitchResident(settings.Runtime); err != nil {
		return err
	}

	a.mu.Lock()
	resident, manager, closed, started := a.companion, a.tasks, a.closed, a.started
	a.mu.Unlock()
	if closed {
		return errors.New(a.text("appStopped"))
	}
	guard := func() error { return a.guardRuntimeChange() }
	if manager != nil {
		if err = manager.PauseIfIdle(a.guardConversationChange); err != nil {
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
	var displaced retainedWorker
	apply := func(binding backend.RuntimeBinding, directory string) error {
		if a.host.RuntimeChanged != nil {
			if e := a.host.RuntimeChanged(directory, first); e != nil {
				return e
			}
		}
		a.Backend.BindRuntime(binding)
		a.setEngine(binding.Engine)
		if previous := a.localWork.SwitchResident(binding.Settings.Runtime, binding.Engine); previous != nil {
			displaced = previous
		}
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
				err = a.localWork.SelectFirstRuntime(settings.Runtime)
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
		if displaced != nil {
			closeRetainedWorker(displaced)
		}
		closeRuntimeEngine(old.Engine)
		return nil
	}

	// No durable active selection was changed on handshake failure. Restore
	// the original resident Bot from its same identity and receipt ledgers.
	if workerDefaultChanged {
		err = errors.Join(err, a.localWork.SelectFirstRuntime(oldWorkerDefault))
	}
	a.stopResidentRuntime()
	closeRuntimeEngine(engine)
	committed = true // the target has already been detached above
	var hostRestore error
	if a.host.RuntimeChanged != nil {
		hostRestore = a.host.RuntimeChanged(oldDirectory, false)
	}
	draftRestore := a.Backend.RestoreRuntimeBinding(old, prepared.Draft.Revision)
	a.setEngine(old.Engine)
	if previous := a.localWork.SwitchResident(old.Settings.Runtime, old.Engine); previous != nil {
		displaced = previous
	}
	startRestore := a.startRuntime()
	a.localWork.RestoreRetained(settings.Runtime, displaced)
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

func closeRetainedWorker(owner retainedWorker) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = owner.Close(ctx)
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
	a.careReady, a.dreamReady = false, false
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
