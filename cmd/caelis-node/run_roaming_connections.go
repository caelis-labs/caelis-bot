package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/app"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
)

// Settings metadata survives optional Worker unavailability. It grants no
// Worker route or ownership: those use only the admitted Runtimes roster.
func roamingNodeRuntimeSettings(c roamingCommand, p roamingWorkerPlan, b api.NodeBackend) (nodeagent.OwnedRuntimeSettings, error) {
	value := nodeagent.OwnedRuntimeSettings{Backend: b}
	if string(b) == c.Backend {
		if b == api.NodeCaelis {
			value.Binary, value.Store = c.CaelisBinary, c.CaelisStore
		} else {
			value.Binary = c.CodexBinary
			if value.Binary == "" {
				value.Binary, _ = exec.LookPath("codex")
			}
		}
	} else {
		bindings := p.SettingsRuntimes
		if bindings == nil {
			bindings = p.Runtimes
		}
		for _, binding := range bindings {
			if binding.Backend == string(b) {
				value.Binary, value.Store = binding.Binary, binding.Store
				break
			}
		}
	}
	if (b != api.NodeCodex && b != api.NodeCaelis) || !filepath.IsAbs(value.Binary) || (b == api.NodeCaelis && !filepath.IsAbs(value.Store)) || (b == api.NodeCodex && value.Store != "") {
		return nodeagent.OwnedRuntimeSettings{}, errors.New("frozen target Runtime settings unavailable")
	}
	return value, nil
}

// This lease borrows settings authority from a live exact managed generation.
// Closing a wizard detaches this lease; it never stops the active Bot's Host.
type roamingNodeConnectionOwner struct {
	holder     *roamingProofOwner
	native     *app.Application
	generation uint64
	settings   api.RuntimeSettings
	done       chan struct{}
	once       sync.Once
}

func (h *roamingProofOwner) nodeConnectionOwner(ctx context.Context, metadata nodeagent.OwnedRuntimeSettings) (nodeagent.NodeConnectionOwner, error) {
	if metadata.Backend != api.NodeCaelis {
		return nil, nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.target.Backend != "caelis" || h.native == nil {
		return nil, nil
	}
	settings, e := app.NativeOwnedCaelisSetupSettings(ctx, h.native, h.target.NodeID)
	if e != nil {
		return nil, e
	}
	if settings.CLIPath != metadata.Binary || settings.CaelisStore != metadata.Store {
		return nil, errors.New("owned node setup designation changed")
	}
	return &roamingNodeConnectionOwner{holder: h, native: h.native, generation: h.generation, settings: settings, done: make(chan struct{})}, nil
}
func (o *roamingNodeConnectionOwner) SetupSettings(ctx context.Context) (api.RuntimeSettings, error) {
	select {
	case <-o.done:
		return api.RuntimeSettings{}, errors.New("original setup observer closed")
	default:
	}
	h := o.holder
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.native != o.native || h.generation != o.generation {
		return api.RuntimeSettings{}, nodecoord.ErrIneligible
	}
	settings, e := app.NativeOwnedCaelisSetupSettings(ctx, o.native, h.target.NodeID)
	if e != nil {
		return api.RuntimeSettings{}, e
	}
	if settings != o.settings {
		return api.RuntimeSettings{}, errors.New("original owned node setup scope changed")
	}
	return settings, nil
}
func (o *roamingNodeConnectionOwner) Check(ctx context.Context) error {
	_, e := o.SetupSettings(ctx)
	return e
}
func (o *roamingNodeConnectionOwner) Done() <-chan struct{} { return o.done }
func (o *roamingNodeConnectionOwner) Close(context.Context) error {
	o.once.Do(func() { close(o.done) })
	return nil
}

func roamingFileDigest(path string) (string, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() > 256<<20 {
		return "", errors.New("trusted native companion unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return "", errors.New("trusted native companion changed")
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
