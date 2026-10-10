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
	native    []api.NativePlugin
	submitted api.Submission
}

func (e pluginReferenceEngine) NativePlugins(context.Context) ([]api.NativePlugin, error) {
	return e.native, nil
}
func (e *pluginReferenceEngine) Submit(_ context.Context, input api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.submitted = input
	return api.Receipt{ID: input.ID, Outcome: "accepted"}, nil
}

func TestComposerPluginReferencesResolveOnlyFromCurrentCatalog(t *testing.T) {
	engine := &pluginReferenceEngine{native: []api.NativePlugin{{ID: "native", Name: "Native Plugin", Source: "codex"}}}
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
	if !strings.Contains(projected.Text, "引用插件：") || strings.Contains(projected.Text, "仅在适合") {
		t.Fatal("plugin selection should render as visible user content")
	}
	for _, id := range []string{"bot-plugin:off", "bot-plugin:removed", "codex-plugin:removed"} {
		_, err = service.resolvePluginReferences(t.Context(), api.Submission{ReferenceIDs: []string{id}})
		if err == nil {
			t.Fatalf("stale or disabled plugin %q was accepted", id)
		}
	}
}

func TestComposerPluginSelectionMatchesOutgoingAndNativeText(t *testing.T) {
	engine := &pluginReferenceEngine{}
	service := NewService(engine, func([]string) ([]api.InputFile, error) { return nil, nil }, nil, nil, nil)
	if err := ConfigureMessageMedia(service, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	service.SetBotPluginSource(func(context.Context) (plugins.Snapshot, error) {
		return plugins.Snapshot{Items: []plugins.Item{{ID: "owned", Title: "Owned Plugin", Installed: true, Enabled: true, Status: "enabled"}}}, nil
	})
	input := api.Submission{ID: "request", Text: "Please help", ReferenceIDs: []string{"bot-plugin:owned"}}
	receipt, err := service.Submit(t.Context(), input)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if len(service.outbox) != 1 || service.outbox[0].item.Text != engine.submitted.Text || !strings.Contains(engine.submitted.Text, `引用插件：Bot: "Owned Plugin"`) {
		t.Fatalf("outgoing text and native input differ: %+v, %q", service.outbox, engine.submitted.Text)
	}
	if len(engine.submitted.ReferenceIDs) != 0 || len(input.ReferenceIDs) != 1 {
		t.Fatal("plugin references were not scoped to one native submission")
	}
}
