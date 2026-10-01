package nodeagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

// OwnedRuntimeSettings is native-only target metadata for an approved owned
// runtime plan. Never project it through product DTOs or the renderer facade.
// It neither proves authentication nor contains credentials or Host authority.
type OwnedRuntimeSettings struct {
	Backend api.NodeBackend `json:"backend"`
	Binary  string          `json:"binary"`
	Store   string          `json:"store"`
}

type ownedRuntimeSettingsRequest struct {
	NodeID  string          `json:"nodeId"`
	Backend api.NodeBackend `json:"backend"`
}

type ownedRuntimeSettingsPort interface {
	ReadOwnedRuntimeSettings(context.Context, string, api.NodeBackend) (OwnedRuntimeSettings, error)
}

func cleanOwnedRuntimePath(path string) bool {
	return len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func validateOwnedRuntimeSettings(value OwnedRuntimeSettings, expected api.NodeBackend) error {
	if !backend(expected) || value.Backend != expected || !cleanOwnedRuntimePath(value.Binary) || value.Backend == api.NodeCodex && value.Store != "" || value.Backend == api.NodeCaelis && !cleanOwnedRuntimePath(value.Store) {
		return errors.New("invalid native owned runtime metadata")
	}
	return nil
}

// ReadOwnedRuntimeSettings reads designated native paths only. It never probes
// account state, reads a Store, creates directories, or starts a native Host.
func (s *Service) ReadOwnedRuntimeSettings(ctx context.Context, nodeID string, b api.NodeBackend) (OwnedRuntimeSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readOwnedRuntimeSettings(ctx, nodeID, b)
}

// readOwnedRuntimeSettings runs under the native configuration/catalog lock;
// configuration callbacks use it without reacquiring that same lock.
func (s *Service) readOwnedRuntimeSettings(ctx context.Context, nodeID string, b api.NodeBackend) (OwnedRuntimeSettings, error) {
	if err := ctx.Err(); err != nil {
		return OwnedRuntimeSettings{}, err
	}
	if nodeID != s.options.NodeID || !backend(b) {
		return OwnedRuntimeSettings{}, errors.New("native owned runtime scope changed")
	}
	options, installation := s.options, s.installation
	value := OwnedRuntimeSettings{Backend: b}
	var err error
	if options.OwnedRuntimeSettings != nil {
		value, err = options.OwnedRuntimeSettings(ctx, b)
		if err != nil {
			return OwnedRuntimeSettings{}, errors.New("native owned runtime metadata unavailable")
		}
	} else {
		settings, e := loadMachineRuntimeSettings(options.Directory, b)
		if e == nil {
			value.Binary, value.Store = settings.CLIPath, settings.CaelisStore
		} else if !errors.Is(e, os.ErrNotExist) {
			return OwnedRuntimeSettings{}, e
		}
		if manager, ok := installation.(interface{ BinaryPath(string) (string, error) }); ok && value.Binary == "" {
			value.Binary, _ = manager.BinaryPath(string(b))
		}
		if value.Binary == "" {
			value.Binary = options.Binaries[b]
		}
		if value.Binary == "" {
			switch config := options.Configurations[b].(type) {
			case *CodexConfiguration:
				if b == api.NodeCodex && config != nil {
					value.Binary = config.Binary
				}
			case *CaelisConfiguration:
				if b == api.NodeCaelis && config != nil {
					value.Binary = config.Settings.CLIPath
				}
			}
		}
		if value.Binary == "" {
			// Match native setup's standard installed CLI discovery. Only an
			// executable path is resolved; no process or account probe is run.
			if b == api.NodeCodex {
				value.Binary, err = codex.ResolveInstalledExecutable("")
			} else {
				value.Binary, err = caelisruntime.Find("")
			}
			if err != nil {
				return OwnedRuntimeSettings{}, errors.New("native owned runtime executable unavailable")
			}
		}
		if b == api.NodeCaelis && value.Store == "" {
			value.Store = filepath.Join(options.Directory, "caelis-store")
			if config, ok := options.Configurations[b].(*CaelisConfiguration); ok && config != nil && config.Settings.CaelisStore != "" {
				value.Store = config.Settings.CaelisStore
			}
		}
	}
	if err := validateOwnedRuntimeSettings(value, b); err != nil {
		return OwnedRuntimeSettings{}, err
	}
	info, err := os.Stat(value.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return OwnedRuntimeSettings{}, errors.New("native owned runtime executable unavailable")
	}
	return value, nil
}

// ReadOwnedRuntimeSettings reads only the retained agent's exact target. Remote
// paths are validated as metadata; they are never opened on the caller's host.
func (c *Client) ReadOwnedRuntimeSettings(ctx context.Context, nodeID string, b api.NodeBackend) (OwnedRuntimeSettings, error) {
	if nodeID != c.expected || !backend(b) {
		return OwnedRuntimeSettings{}, errors.New("native owned runtime scope changed")
	}
	var out OwnedRuntimeSettings
	if err := c.request(ctx, "POST", "/v1/node/owned-runtime-settings", ownedRuntimeSettingsRequest{NodeID: nodeID, Backend: b}, &out); err != nil {
		return OwnedRuntimeSettings{}, err
	}
	if err := validateOwnedRuntimeSettings(out, b); err != nil {
		return OwnedRuntimeSettings{}, err
	}
	return out, nil
}

func machineRuntimePath(directory string, b api.NodeBackend) string {
	return filepath.Join(directory, "runtime-"+string(b)+".json")
}
func loadMachineRuntimeSettings(directory string, b api.NodeBackend) (api.RuntimeSettings, error) {
	var value api.RuntimeSettings
	err := readPrivateJSON(machineRuntimePath(directory, b), &value)
	if err != nil {
		return value, err
	}
	if value.Runtime != string(b) || validateOwnedRuntimeSettings(OwnedRuntimeSettings{Backend: b, Binary: value.CLIPath, Store: value.CaelisStore}, b) != nil {
		return value, errors.New("private machine Runtime settings invalid")
	}
	return value, nil
}

// SaveOwnedRuntimeSettings changes only the native Node's nonsecret designation.
// It verifies the concrete executable; no shell, login or Runtime owner is changed.
func (s *Service) SaveOwnedRuntimeSettings(ctx context.Context, r api.NodeRuntimeSettingsRequest) (api.RuntimeCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.options.OwnedRuntimeSettings != nil || r.Guard.NodeID != s.options.NodeID || !backend(r.Guard.Backend) || r.Settings.Runtime != string(r.Guard.Backend) {
		return api.RuntimeCheck{}, errors.New("machine Runtime scope unavailable")
	}
	current, err := s.configuration(ctx, r.Guard.NodeID, r.Guard.Backend)
	if err != nil || current.Guard != r.Guard {
		return api.RuntimeCheck{}, errors.New("machine Runtime view changed")
	}
	value := OwnedRuntimeSettings{Backend: r.Guard.Backend, Binary: r.Settings.CLIPath, Store: r.Settings.CaelisStore}
	if validateOwnedRuntimeSettings(value, r.Guard.Backend) != nil {
		return api.RuntimeCheck{}, errors.New("explicit machine Runtime paths required")
	}
	if _, err = s.detectExecutableVersion(ctx, r.Guard.Backend, value.Binary); err != nil {
		return api.RuntimeCheck{}, errors.New("machine Runtime executable check failed")
	}
	if err = writeState(machineRuntimePath(s.options.Directory, r.Guard.Backend), r.Settings); err != nil {
		return api.RuntimeCheck{}, err
	}
	s.options.Binaries[r.Guard.Backend] = value.Binary
	switch c := s.options.Configurations[r.Guard.Backend].(type) {
	case *CodexConfiguration:
		c.Binary = value.Binary
	case *CaelisConfiguration:
		c.Settings = r.Settings
	}
	return api.RuntimeCheck{Saved: true, Message: "Machine Runtime path saved"}, nil
}
func (c *Client) SaveOwnedRuntimeSettings(ctx context.Context, r api.NodeRuntimeSettingsRequest) (api.RuntimeCheck, error) {
	if r.Guard.NodeID != c.expected {
		return api.RuntimeCheck{}, errors.New("machine Runtime scope changed")
	}
	var value api.RuntimeCheck
	err := c.request(ctx, "POST", "/v1/node/runtime-settings", r, &value)
	return value, err
}
