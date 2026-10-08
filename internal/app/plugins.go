package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

type pluginDetailCacheEntry struct {
	detail  plugins.ServerDetail
	expires time.Time
}

func (a *Application) PluginSkillDetail(_ context.Context, id, skill string) (plugins.SkillDetail, error) {
	if a.plugins == nil {
		return plugins.SkillDetail{}, errors.New("plugin store unavailable")
	}
	return a.plugins.SkillDetail(id, skill)
}

func (a *Application) PluginServerDetail(ctx context.Context, id, server string, refresh bool) (plugins.ServerDetail, error) {
	if a.plugins == nil {
		return plugins.ServerDetail{}, errors.New("plugin store unavailable")
	}
	snapshot := a.plugins.Snapshot()
	var item *plugins.Item
	for i := range snapshot.Items {
		if snapshot.Items[i].ID == id {
			item = &snapshot.Items[i]
			break
		}
	}
	if item == nil {
		return plugins.ServerDetail{}, errors.New("plugin is not reviewed")
	}
	found := false
	for _, contribution := range item.MCPServers {
		if contribution.ID == server {
			found = true
			break
		}
	}
	if !found {
		return plugins.ServerDetail{}, errors.New("server is not reviewed")
	}
	if !item.Installed {
		return plugins.ServerDetail{State: "not_configured", Tools: []plugins.Tool{}}, nil
	}
	if !item.Enabled {
		return plugins.ServerDetail{State: "disabled", Tools: []plugins.Tool{}}, nil
	}
	selected := false
	for _, entry := range a.plugins.Selection().Servers {
		if entry.PackageID == id && entry.Name == server {
			selected = true
			break
		}
	}
	if !selected {
		return plugins.ServerDetail{State: "not_configured", Tools: []plugins.Tool{}}, nil
	}
	inspector, ok := a.engine.(api.PluginInspector)
	if !ok {
		return plugins.ServerDetail{State: "unknown", Tools: []plugins.Tool{}}, nil
	}
	key := fmt.Sprintf("%d/%d/%s/%s/%s", snapshot.Revision, inspector.BotPluginGeneration(), id, item.Version, server)
	a.pluginDetailMu.Lock()
	if cached, ok := a.pluginDetailCache[key]; !refresh && ok && time.Now().Before(cached.expires) {
		a.pluginDetailMu.Unlock()
		return cached.detail, nil
	}
	a.pluginDetailMu.Unlock()
	detail, err := inspector.BotPluginServer(ctx, plugins.RuntimeName(id, server))
	if err != nil {
		return plugins.ServerDetail{State: "failed", Tools: []plugins.Tool{}}, nil
	}
	if len(detail.Tools) > 128 {
		detail.Tools = detail.Tools[:128]
	}
	if detail.Tools == nil {
		detail.Tools = []plugins.Tool{}
	}
	if detail.State == "connected" {
		a.pluginDetailMu.Lock()
		if len(a.pluginDetailCache) >= 32 {
			a.pluginDetailCache = nil
		}
		if a.pluginDetailCache == nil {
			a.pluginDetailCache = make(map[string]pluginDetailCacheEntry)
		}
		a.pluginDetailCache[key] = pluginDetailCacheEntry{detail: detail, expires: time.Now().Add(15 * time.Second)}
		a.pluginDetailMu.Unlock()
	}
	return detail, nil
}

func (a *Application) PluginSnapshot(ctx context.Context) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	snapshot := a.plugins.Snapshot()
	if runtime, ok := a.engine.(api.PluginConfigurator); ok {
		selection := a.plugins.Selection()
		owners := map[string]string{}
		for _, server := range selection.Servers {
			owners[plugins.RuntimeName(server.PackageID, server.Name)] = server.PackageID
		}
		for _, issue := range runtime.BotPluginHealth(ctx) {
			for i := range snapshot.Items {
				item := &snapshot.Items[i]
				matches := issue.Name == item.ID || owners[issue.Name] == item.ID || issue.Component == "skill" && strings.Contains(issue.Name, "/"+item.ID+"/") || issue.Component == "runtime" && item.Enabled
				if matches {
					item.Issues = append(item.Issues, issue)
					if item.Enabled {
						item.Status = "failed"
					}
				}
			}
		}
	}
	return snapshot, nil
}
func (a *Application) PluginAction(ctx context.Context, id, action string) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	adapter, ok := a.engine.(api.PluginConfigurator)
	if !ok {
		return plugins.Snapshot{}, errors.New("runtime does not support Bot plugins")
	}
	a.pluginAdmission.Lock()
	defer a.pluginAdmission.Unlock()
	mutationErr := adapter.WithBotPluginAdmission(func(apply func(context.Context, plugins.Selection) error) error {
		// Runtime admission is acquired before the application lock, just as it
		// is for PrepareTurn. Keep admission through the private store commit.
		a.mu.Lock()
		started, closed := a.started, a.closed
		a.mu.Unlock()
		if closed {
			return errors.New("Bot has stopped")
		}
		if !started {
			apply = nil
		}
		_, err := a.plugins.Mutate(ctx, id, action, apply)
		return err
	})
	if mutationErr != nil {
		snapshot, _ := a.PluginSnapshot(ctx)
		return snapshot, mutationErr
	}
	a.pluginDetailMu.Lock()
	a.pluginDetailCache = nil
	a.pluginDetailMu.Unlock()
	return a.PluginSnapshot(ctx)
}

// PluginConnection is write-only at the desktop boundary. The credential
// stays in Bot's native secret store; only a revision enters Runtime config.
func (a *Application) PluginConnection(ctx context.Context, id, secret, caPEM string, clear bool) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	adapter, ok := a.engine.(api.PluginConfigurator)
	if !ok {
		return plugins.Snapshot{}, errors.New("runtime does not support Bot plugins")
	}
	a.pluginAdmission.Lock()
	defer a.pluginAdmission.Unlock()
	err := adapter.WithBotPluginAdmission(func(apply func(context.Context, plugins.Selection) error) error {
		a.mu.Lock()
		started, closed := a.started, a.closed
		a.mu.Unlock()
		if closed {
			return errors.New("Bot has stopped")
		}
		if !started {
			apply = nil
		}
		_, e := a.plugins.ConfigureConnection(ctx, id, secret, caPEM, clear, apply)
		return e
	})
	if err != nil {
		snapshot, _ := a.PluginSnapshot(ctx)
		return snapshot, err
	}
	a.pluginDetailMu.Lock()
	a.pluginDetailCache = nil
	a.pluginDetailMu.Unlock()
	return a.PluginSnapshot(ctx)
}
