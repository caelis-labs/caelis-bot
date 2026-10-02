package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

// Node settings designate a private slot when there is no explicit Caelis
// profile. Ordinary local runtimeSetup keeps its original profile/default Store.
// This resolver is passive: only an explicit target Begin may prepare the slot.
func nodeLocalCaelisSettings(a *Application, directory string, explicit *api.RuntimeSettings) (api.RuntimeSettings, error) {
	var settings api.RuntimeSettings
	if explicit != nil {
		settings = *explicit
	} else {
		if a == nil {
			return settings, errors.New("actual local Caelis profile unavailable")
		}
		// The outer native profile owns this machine's designation. A fresh
		// imported generation contains portable Notebook data, not an alternate
		// Runtime's settings. A product facade can also point at another node.
		profile := filepath.Join(a.root, "runtime-profiles", "caelis.json")
		if _, e := os.Stat(profile); !errors.Is(e, os.ErrNotExist) {
			settings, e = backend.LoadRuntimeSettings(profile, "caelis")
			if e != nil {
				return settings, e
			}
		} else {
			active, e := backend.LoadRuntimeSettings(filepath.Join(a.root, "runtime.json"), "codex")
			if e != nil {
				return settings, e
			}
			settings = api.RuntimeSettings{Runtime: "caelis"}
			if active.Runtime == "caelis" {
				settings = active
			}
		}
	}
	if settings.Runtime != "caelis" {
		return settings, errors.New("exact local Caelis profile required")
	}
	if settings.CaelisStore == "" {
		settings.CaelisStore = filepath.Join(directory, "caelis-store")
	}
	if settings.CLIPath == "" {
		settings.CLIPath, _ = exec.LookPath("caelis")
	}
	return settings, nil
}

func (n *nativeNodeManagement) localOwnedRuntimeSettings(ctx context.Context, b api.NodeBackend) (nodeagent.OwnedRuntimeSettings, error) {
	if e := ctx.Err(); e != nil {
		return nodeagent.OwnedRuntimeSettings{}, e
	}
	settings := api.RuntimeSettings{Runtime: string(b)}
	var e error
	if b == api.NodeCaelis {
		settings, e = nodeLocalCaelisSettings(n.app, filepath.Join(n.directory, "local"), n.options.LocalCaelisSettings)
	} else if b == api.NodeCodex {
		actual := n.app
		if n.options.LocalCodexBinary != "" {
			settings.CLIPath = n.options.LocalCodexBinary
		} else if actual != nil && actual.Backend != nil {
			settings, e = actual.Backend.SetupProfile("codex")
		}
		if e == nil && settings.CLIPath == "" {
			settings.CLIPath, _ = exec.LookPath("codex")
		}
	} else {
		e = errors.New("unsupported local native Runtime")
	}
	if e == nil && n.localInstaller != nil {
		// Explicitly installed Node-owned bytes affect this native slot only;
		// ordinary APP/provider profiles and the user's external CLI stay intact.
		if binary, err := n.localInstaller.BinaryPath(string(b)); err == nil {
			settings.CLIPath = binary
		}
	}
	store := ""
	if b == api.NodeCaelis {
		store = settings.CaelisStore
	}
	return nodeagent.OwnedRuntimeSettings{Backend: b, Binary: settings.CLIPath, Store: store}, e
}
func (n *nativeNodeManagement) localOwnedRuntimeCompanion(ctx context.Context) (nodeagent.OwnedRuntimeCompanion, error) {
	if e := ctx.Err(); e != nil {
		return nodeagent.OwnedRuntimeCompanion{}, e
	}
	path := n.options.JoinHelperPath
	var e error
	if path == "" {
		path, e = DefaultNativeNodeHost()
	}
	if e != nil {
		return nodeagent.OwnedRuntimeCompanion{}, e
	}
	digest, e := nativeCompanionDigest(path)
	return nodeagent.OwnedRuntimeCompanion{Path: path, SHA256: digest}, e
}
