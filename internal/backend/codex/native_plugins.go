package codex

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// NativePlugins lists only Codex plugins already installed and enabled for
// this user. A menu reference is a per-turn hint, not an installation action.
func (s *Session) NativePlugins(ctx context.Context) ([]api.NativePlugin, error) {
	s.mu.Lock()
	client, directory := s.client, s.opts.Directory
	s.mu.Unlock()
	if client == nil {
		return []api.NativePlugin{}, nil
	}
	raw, err := client.rpc.observe(ctx, "plugin/installed", map[string]any{"cwds": []string{directory}})
	if err != nil {
		return nil, err
	}
	return projectNativePlugins(raw)
}

func projectNativePlugins(raw []byte) ([]api.NativePlugin, error) {
	var catalog struct {
		Marketplaces []struct {
			Plugins []struct {
				ID, Name           string
				Installed, Enabled bool
				Interface          *struct{ DisplayName, ShortDescription string }
			}
		}
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	out := []api.NativePlugin{}
	seen := map[string]bool{}
	for _, marketplace := range catalog.Marketplaces {
		for _, plugin := range marketplace.Plugins {
			if !plugin.Installed || !plugin.Enabled || plugin.ID == "" || seen[plugin.ID] {
				continue
			}
			seen[plugin.ID] = true
			name, description := plugin.Name, ""
			if plugin.Interface != nil {
				if plugin.Interface.DisplayName != "" {
					name = plugin.Interface.DisplayName
				}
				description = plugin.Interface.ShortDescription
			}
			name = boundedPluginLabel(name, 80)
			if name == "" {
				continue
			}
			out = append(out, api.NativePlugin{ID: plugin.ID, Name: name, Description: boundedPluginLabel(description, 160), Source: "codex"})
		}
	}
	return out, nil
}

func boundedPluginLabel(value string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
