package nodeagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
	if err := ctx.Err(); err != nil {
		return OwnedRuntimeSettings{}, err
	}
	if nodeID != s.options.NodeID || !backend(b) {
		return OwnedRuntimeSettings{}, errors.New("native owned runtime scope changed")
	}
	s.mu.Lock()
	options, installation := s.options, s.installation
	s.mu.Unlock()
	value := OwnedRuntimeSettings{Backend: b}
	var err error
	if options.OwnedRuntimeSettings != nil {
		value, err = options.OwnedRuntimeSettings(ctx, b)
		if err != nil {
			return OwnedRuntimeSettings{}, errors.New("native owned runtime metadata unavailable")
		}
	} else {
		if manager, ok := installation.(interface{ BinaryPath(string) (string, error) }); ok {
			value.Binary, _ = manager.BinaryPath(string(b))
		}
		if value.Binary == "" {
			value.Binary = options.Binaries[b]
		}
		if b == api.NodeCaelis {
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
