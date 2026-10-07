package app

import (
	"context"
	"errors"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

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
	a.mu.Lock()
	var apply func(context.Context, plugins.Selection) error
	if a.started {
		apply = adapter.UpdateBotPlugins
	}
	_, mutationErr := a.plugins.Mutate(ctx, id, action, apply)
	a.mu.Unlock()
	if mutationErr != nil {
		snapshot, _ := a.PluginSnapshot(ctx)
		return snapshot, mutationErr
	}
	return a.PluginSnapshot(ctx)
}
