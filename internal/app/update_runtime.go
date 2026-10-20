package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
	"github.com/caelis-labs/caelis-bot/internal/managedupdate"
	"github.com/caelis-labs/caelis-bot/internal/updates"
)

// ManagedRuntime prepares only an already used, selected Caelis installation.
// Core's public owner chooses/starts the Host and defers busy upgrades. Later
// calls are read-only observations, including when another surface changes it.
func (a *Application) ManagedRuntime(ctx context.Context, prepare bool) (managedupdate.Runtime, error) {
	out := managedupdate.Runtime{State: "inactive"}
	v := a.Backend.RuntimeSettings()
	if v.Runtime != "caelis" {
		return out, nil
	}
	store, err := caelisruntime.Store(v.CaelisStore)
	if err != nil {
		return out, err
	}
	_, discoveryErr := os.Stat(filepath.Join(store, "runtime/service/discovery.json"))
	_, bindingErr := os.Stat(filepath.Join(a.ProviderDirectory(), "application.json"))
	if os.IsNotExist(discoveryErr) && os.IsNotExist(bindingErr) {
		return out, nil // Missing Runtime must not prevent first-run settings.
	}
	out.State, out.Reason = "failed", "runtime_start_failed"
	cli, err := caelisruntime.Inspect(ctx, v.CLIPath)
	if err != nil || !cli.Installed {
		return out, err
	}
	out.BundledVersion = cli.Version
	if prepare {
		if err = caelisruntime.PrepareBundled(ctx, v.CLIPath, v.CaelisStore); err != nil {
			return out, err
		}
	}
	out.Reason = "runtime_readback_failed"
	host, err := caelis.InspectHost(ctx, v)
	if err != nil {
		return out, err
	}
	out.HostVersion, out.HostBuild, out.HostInstance = host.Version, host.Build, host.Instance
	out.State, out.Reason = "ready", ""
	if cli.Bundled {
		if cmp, err := updates.CompareVersions(host.Version, cli.Version); err != nil {
			out.State, out.Reason = "failed", "runtime_readback_failed"
		} else if cmp < 0 {
			out.State = "pending" // Core accepted the candidate; actual Host is older.
		}
	} else {
		out.State = "external"
	}
	return out, nil
}
