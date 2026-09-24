package app

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

func (a *Application) configureRuntimeManagement() {
	a.Backend.ConfigureRuntimeManagement([]api.ProviderInfo{(&codex.Session{}).ProviderInfo(), (&caelis.Session{}).ProviderInfo()}, func(ctx context.Context, v api.RuntimeSettings) error {
		switch v.Runtime {
		case "caelis":
			dir, err := providerDirectory(a.root, v.Runtime)
			if err != nil {
				return err
			}
			return caelis.ProbeBinding(ctx, v, dir)
		case "codex":
			if e := codex.ValidateSettings(v, api.ExecutionSettings{}); e != nil {
				return e
			}
			probe, err := codex.Start(ctx, codex.Options{Binary: v.CLIPath, CLIOnly: v.CLIPath != ""})
			if err != nil {
				return err
			}
			defer probe.Close()
			_, err = probe.ReadAuthStatus(ctx)
			return err
		default:
			return errors.New("不支持的运行时")
		}
	}, func(ctx context.Context, action string, v api.RuntimeSettings) (api.RuntimeStatus, error) {
		if v.Runtime != "caelis" {
			return api.RuntimeStatus{}, errors.New("自动安装与更新仅适用于 Caelis")
		}
		return a.manageCaelis(ctx, action, v)
	}, a.guardRuntimeChange)
}
func (a *Application) guardRuntimeChange() error {
	if err := a.guardConversationChange(); err != nil {
		return err
	}
	a.mu.Lock()
	tasks := a.tasks
	a.mu.Unlock()
	if tasks != nil {
		for _, t := range tasks.ListTasks() {
			switch t.Status {
			case "completed", "failed", "cancelled", "interrupted":
			default:
				return errors.New("仍有未结束的独立工作")
			}
		}
	}
	return a.guardReminderChange()
}

func (a *Application) guardConversationChange() error {
	if err := a.initialization.GuardRuntimeChange(); err != nil {
		return err
	}
	v := a.engine.Snapshot()
	if v.CanInterrupt || len(v.Approvals) > 0 || v.Phase == "unknown" || v.Phase == "sending" {
		return errors.New("请等待工作结束并核对待处理操作")
	}
	return nil
}

func (a *Application) guardReminderChange() error {
	a.mu.Lock()
	resident := a.companion
	a.mu.Unlock()
	if resident != nil {
		if w := resident.State().Wake; w != nil && w.Runtime == a.engine.(api.Provider).ProviderInfo().ID && w.Status != "accepted" {
			return errors.New("仍有待核对的提醒")
		}
	}
	return nil
}
