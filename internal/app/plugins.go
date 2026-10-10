package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
		if item.Connection != nil && item.Connection.State == "authentication_required" {
			return plugins.ServerDetail{State: "authentication_required", Tools: []plugins.Tool{}}, nil
		}
		if item.Connection != nil && item.Connection.Stored {
			return plugins.ServerDetail{State: "not_started", Tools: []plugins.Tool{}, Error: "Saved connection is not in the active Bot Runtime selection"}, nil
		}
		return plugins.ServerDetail{State: "not_configured", Tools: []plugins.Tool{}}, nil
	}
	if runtime := a.engine.Snapshot(); runtime.ConnectionIssue == "workspace_trust" {
		return plugins.ServerDetail{State: "trust_blocked", Tools: []plugins.Tool{}, Error: runtime.Message}, nil
	}
	a.mu.Lock()
	started := a.started
	a.mu.Unlock()
	a.pluginSyncMu.Lock()
	unsynced := started && snapshot.Revision != a.pluginSyncRevision
	failed := a.pluginSyncError && !a.pluginSyncRunning
	a.pluginSyncMu.Unlock()
	if unsynced {
		if failed {
			return plugins.ServerDetail{State: "failed", Tools: []plugins.Tool{}}, nil
		}
		return plugins.ServerDetail{State: "pending", Tools: []plugins.Tool{}}, nil
	}
	inspector, hasInspector := a.engine.(api.PluginInspector)
	var generation uint64
	if hasInspector {
		generation = inspector.BotPluginGeneration()
	}
	key := fmt.Sprintf("%d/%d/%s/%s/%s", snapshot.Revision, generation, id, item.Version, server)
	a.pluginDetailMu.Lock()
	if cached, ok := a.pluginDetailCache[key]; !refresh && ok && time.Now().Before(cached.expires) {
		a.pluginDetailMu.Unlock()
		return cached.detail, nil
	}
	a.pluginDetailMu.Unlock()
	detail := plugins.ServerDetail{State: "not_started", Tools: []plugins.Tool{}}
	if hasInspector {
		var err error
		detail, err = inspector.BotPluginServer(ctx, plugins.RuntimeName(id, server))
		if err != nil {
			return plugins.ServerDetail{State: "failed", Error: "Could not read the Bot Runtime MCP directory", Tools: []plugins.Tool{}}, nil
		}
	}
	if detail.State == "not_started" {
		if catalog := a.plugins.ProbeServer(ctx, id, server); catalog.State == "connected" {
			// A standalone preview cannot establish the resident Runtime's health.
			detail.Tools, detail.Truncated, detail.Preview = catalog.Tools, catalog.Truncated, true
		}
	} else if detail.State == "connected" && needsToolMetadata(detail.Tools) {
		// Older Core status may omit descriptions. Read metadata only when
		// this detail is explicitly opened,
		// and never expose a tool absent from the Runtime's active directory.
		if catalog := a.plugins.ProbeServer(ctx, id, server); catalog.State == "connected" {
			detail.Tools = append([]plugins.Tool(nil), detail.Tools...)
			enrichToolMetadata(detail.Tools, catalog.Tools)
		}
	}
	if a.plugins.Snapshot().Revision != snapshot.Revision {
		return plugins.ServerDetail{State: "unknown", Tools: []plugins.Tool{}}, nil
	}
	if len(detail.Tools) > 128 {
		detail.Tools = detail.Tools[:128]
		detail.Truncated = true
	}
	if detail.Tools == nil {
		detail.Tools = []plugins.Tool{}
	}
	if detail.State == "connected" || detail.Preview {
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
	if refresh {
		if err := a.syncPluginIndex(ctx); err != nil && a.host.ReportError != nil {
			a.host.ReportError(err)
		}
	}
	return detail, nil
}

func needsToolMetadata(tools []plugins.Tool) bool {
	for _, tool := range tools {
		if tool.Description == "" {
			return true
		}
	}
	return false
}

func enrichToolMetadata(active, discovered []plugins.Tool) {
	byName := make(map[string]plugins.Tool, len(discovered))
	for _, tool := range discovered {
		byName[tool.Name] = tool
	}
	for i := range active {
		metadata, ok := byName[active[i].Name]
		if !ok {
			continue
		}
		if active[i].Title == "" {
			active[i].Title = metadata.Title
		}
		if active[i].Description == "" {
			active[i].Description = metadata.Description
		}
		if active[i].ReadOnlyHint == nil {
			active[i].ReadOnlyHint = metadata.ReadOnlyHint
		}
		if active[i].DestructiveHint == nil {
			active[i].DestructiveHint = metadata.DestructiveHint
		}
		if active[i].IdempotentHint == nil {
			active[i].IdempotentHint = metadata.IdempotentHint
		}
		if active[i].OpenWorldHint == nil {
			active[i].OpenWorldHint = metadata.OpenWorldHint
		}
	}
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
					if item.Enabled && item.Status != "update_available" {
						item.Status = "failed"
					}
				}
			}
		}
	}
	return a.pluginDesiredView(snapshot), nil
}

func (a *Application) pluginDesiredView(snapshot plugins.Snapshot) plugins.Snapshot {
	a.mu.Lock()
	started := a.started
	a.mu.Unlock()
	a.pluginSyncMu.Lock()
	unsynced := started && snapshot.Revision != a.pluginSyncRevision
	running, failed := a.pluginSyncRunning, a.pluginSyncError
	a.pluginSyncMu.Unlock()
	if unsynced {
		if failed && !running {
			snapshot.SyncState = "failed"
		} else {
			snapshot.SyncState = "pending"
		}
		for i := range snapshot.Items {
			item := &snapshot.Items[i]
			if !item.Installed || !item.Enabled || item.Status == "update_available" || item.Status == "needs_connection" || item.Status == "unavailable" {
				continue
			}
			if failed && !running {
				item.Status = "failed"
				item.Issues = append(item.Issues, plugins.Issue{Component: "runtime", Name: item.ID, Message: "Plugin Runtime update needs recovery"})
			} else {
				item.Status = "pending"
			}
		}
	}
	return snapshot
}
func (a *Application) PluginAction(ctx context.Context, id, action string) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return plugins.Snapshot{}, errors.New("Bot has stopped")
	}
	// Package management commits independently of the model, Worker, and
	// Runtime. Reconciliation is automatic and cannot turn a confirmed install
	// into an unknown install receipt.
	snapshot, err := a.plugins.Mutate(ctx, id, action, nil)
	if err != nil {
		return snapshot, err
	}
	a.pluginDetailMu.Lock()
	a.pluginDetailCache = nil
	a.pluginDetailMu.Unlock()
	if action == "uninstall" || action == "disable" {
		a.queuePluginIndex()
	}
	a.queuePluginReconcile()
	return a.pluginDesiredView(a.plugins.Snapshot()), nil
}

// PluginConnection is write-only at the desktop boundary. The credential
// stays in Bot's native secret store; only a revision enters Runtime config.
func (a *Application) PluginConnection(ctx context.Context, id, secret, caPEM string, clear bool) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return plugins.Snapshot{}, errors.New("Bot has stopped")
	}
	snapshot, err := a.plugins.ConfigureConnection(ctx, id, secret, caPEM, clear, nil)
	if err != nil {
		return snapshot, err
	}
	a.pluginDetailMu.Lock()
	a.pluginDetailCache = nil
	a.pluginDetailMu.Unlock()
	if clear {
		a.queuePluginIndex()
	}
	a.queuePluginReconcile()
	return a.pluginDesiredView(a.plugins.Snapshot()), nil
}

func (a *Application) PluginOAuthStart(ctx context.Context, id string) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return plugins.Snapshot{}, errors.New("Bot has stopped")
	}
	return a.plugins.BeginOAuth(ctx, id, a.host.OpenURL, func(flowCtx context.Context, pluginID, state string, grant plugins.OAuthGrant) error {
		a.mu.Lock()
		closed := a.closed
		a.mu.Unlock()
		if closed {
			return errors.New("Bot has stopped")
		}
		_, err := a.plugins.ConfigureOAuthGrant(flowCtx, pluginID, state, grant, nil)
		if err == nil {
			a.pluginDetailMu.Lock()
			a.pluginDetailCache = nil
			a.pluginDetailMu.Unlock()
			a.queuePluginReconcile()
		}
		return err
	})
}

func (a *Application) PluginOAuthCancel(_ context.Context, id string) (plugins.Snapshot, error) {
	if a.plugins == nil {
		return plugins.Snapshot{}, errors.New("plugin store unavailable")
	}
	return a.plugins.CancelOAuth(id), nil
}

// syncPluginIndex reconciles only Runtime-confirmed connected services. It
// never publishes a raw package declaration or a failed/unknown connection.
func (a *Application) syncPluginIndex(ctx context.Context) error {
	if a.plugins == nil || a.skillPath == "" {
		return nil
	}
	a.pluginIndexMu.Lock()
	defer a.pluginIndexMu.Unlock()
	path := filepath.Join(filepath.Dir(filepath.Dir(a.skillPath)), "mcp-tools.json")
	inspector, ok := a.engine.(api.PluginInspector)
	if !ok {
		return plugins.WriteIndex(path, nil)
	}
	selection := a.plugins.Selection()
	generation := inspector.BotPluginGeneration()
	connected := make([]plugins.IndexServer, 0, len(selection.Servers))
	for _, server := range selection.Servers {
		name := plugins.RuntimeName(server.PackageID, server.Name)
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		detail, err := inspector.BotPluginServer(probeCtx, name)
		cancel()
		if err != nil || detail.State != "connected" {
			continue
		}
		connected = append(connected, plugins.IndexServer{PackageID: server.PackageID, Name: server.Name, RuntimeName: name, Tools: detail.Tools})
	}
	if a.plugins.Selection().Revision != selection.Revision || inspector.BotPluginGeneration() != generation {
		return plugins.WriteIndex(path, nil)
	}
	return plugins.WriteIndex(path, connected)
}
