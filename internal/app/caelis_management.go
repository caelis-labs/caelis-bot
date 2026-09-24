package app

import (
	"context"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func runtimeInstallation(st caelisruntime.Status) api.RuntimeStatus {
	return api.RuntimeStatus{Installed: st.Installed, Path: st.Path, Version: st.Version, Message: st.Message, LatestVersion: st.LatestVersion, UpdateState: st.UpdateState}
}

// All user-facing Caelis installation paths converge here. Installation alone
// is not readiness: select the installed Host, negotiate, then reconnect Bot.
func (a *Application) manageCaelis(ctx context.Context, action string, v api.RuntimeSettings) (api.RuntimeStatus, error) {
	if action == "detect" || action == "check-update" {
		st, err := caelisruntime.Manage(ctx, action, v.CLIPath, v.CaelisStore, a.locale())
		return runtimeInstallation(st), err
	}
	if err := a.PrepareUpdate(); err != nil {
		return api.RuntimeStatus{}, err
	}
	defer a.CancelUpdate()
	if err := caelis.CheckServiceIdle(ctx, v); err != nil {
		return api.RuntimeStatus{}, err
	}
	st, err := caelisruntime.Manage(ctx, action, v.CLIPath, v.CaelisStore, a.locale())
	if err != nil {
		return runtimeInstallation(st), err
	}
	if action == "install" || action == "update" {
		// Another client may have started work during a download. Retain the new
		// binary and let the user apply it later; never repeat the installation.
		if err = caelis.CheckServiceIdle(ctx, v); err != nil {
			return runtimeInstallation(st), err
		}
		st, err = caelisruntime.Manage(ctx, "apply-update", v.CLIPath, v.CaelisStore, a.locale())
		if err != nil {
			return runtimeInstallation(st), fmt.Errorf("%s: %w", a.text("host.programInstalledServiceNotStarted"), err)
		}
	}
	if err = caelis.Probe(ctx, v); err != nil {
		return runtimeInstallation(st), fmt.Errorf("%s: %w", a.text("host.serviceConnectionNotVerified"), err)
	}
	active := a.Backend.RuntimeSettings()
	store, _ := caelisruntime.Store(v.CaelisStore)
	activeStore, _ := caelisruntime.Store(active.CaelisStore)
	if session, ok := a.engine.(*caelis.Session); ok && store == activeStore {
		if err = session.Reconnect(ctx); err != nil {
			return runtimeInstallation(st), fmt.Errorf("%s: %w", a.text("host.serviceReadyBotReconnectPending"), err)
		}
	}
	st.Message = a.text("host.caelisServiceReady")
	return runtimeInstallation(st), nil
}
