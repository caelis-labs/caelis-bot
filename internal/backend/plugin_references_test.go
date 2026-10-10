package backend

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

type pluginReferenceEngine struct {
	snapshotEngine
	native []api.NativePlugin
}

func (e pluginReferenceEngine) NativePlugins(context.Context) ([]api.NativePlugin, error) {
	return e.native, nil
}

func TestComposerPluginReferencesResolveOnlyFromCurrentCatalog(t *testing.T) {
	engine := pluginReferenceEngine{native: []api.NativePlugin{{ID: "native", Name: "Native Plugin", Source: "codex"}}}
	service := NewService(engine, nil, nil, nil, nil)
	service.SetBotPluginSource(func(context.Context) (plugins.Snapshot, error) {
		return plugins.Snapshot{Items: []plugins.Item{{ID: "owned", Title: "Owned Plugin", Installed: true, Enabled: true, Status: "enabled"}, {ID: "off", Title: "Disabled", Installed: true, Status: "disabled"}}}, nil
	})
	original := api.Submission{ID: "request", Text: "Please help", ReferenceIDs: []string{"bot-plugin:owned", "codex-plugin:native", "existing-skill"}}
	projected, err := service.resolvePluginReferences(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.ReferenceIDs) != 1 || projected.ReferenceIDs[0] != "existing-skill" {
		t.Fatalf("native references = %v", projected.ReferenceIDs)
	}
	if !strings.Contains(projected.Text, `Bot: "Owned Plugin"`) || !strings.Contains(projected.Text, `Codex: "Native Plugin"`) || original.Text != "Please help" || len(original.ReferenceIDs) != 3 {
		t.Fatal("submission/draft identity changed or selection was lost")
	}
	for _, id := range []string{"bot-plugin:off", "bot-plugin:removed", "codex-plugin:removed"} {
		_, err = service.resolvePluginReferences(t.Context(), api.Submission{ReferenceIDs: []string{id}})
		if err == nil {
			t.Fatalf("stale or disabled plugin %q was accepted", id)
		}
	}
}
