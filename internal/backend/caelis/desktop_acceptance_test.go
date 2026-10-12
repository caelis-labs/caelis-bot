package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContentV1DoesNotDuplicateStructuredState(t *testing.T) {
	state := map[string]any{"observation": "one-copy", "value": 9007199254740993}
	fallback := map[string]string{"type": "text", "text": `{ "value": 9007199254740993, "observation": "one-copy" }`}
	image := map[string]string{"type": "image", "mimeType": "image/png", "data": "fixture"}
	prose := map[string]string{"type": "text", "text": "Actual additional context"}
	other := map[string]string{"type": "text", "text": `{"observation":"different"}`}
	result := api.ToolResult{StructuredContent: state, Content: []map[string]string{fallback, image, prose, other}}
	if got := contentV1Blocks(result); !reflect.DeepEqual(got, []map[string]string{image, prose, other}) {
		t.Fatal("duplicate state remained, or unique content/media was dropped")
	}
	if len(result.Content) != 4 || result.Content[0]["text"] != fallback["text"] {
		t.Fatal("projection mutated the original tool result")
	}
	result.Content = []map[string]string{fallback}
	if got := contentV1Blocks(result); len(got) != 1 || strings.Contains(got[0]["text"], "one-copy") {
		t.Fatal("text-only receipt must remain valid without repeating the JSON")
	}
	result.StructuredContent = nil
	if got := contentV1Blocks(result); !reflect.DeepEqual(got, result.Content) {
		t.Fatal("unstructured content was altered")
	}
}

func TestContentCapabilityAndRestartCatalog(t *testing.T) {
	s := New(Options{Directory: filepath.Join(t.TempDir(), "binding")})
	contentTool := fixtureDefinitions("string")[0]
	contentTool.ResultFormat = "content-v1"
	h := &acceptanceTools{defs: []api.ToolDefinition{contentTool}}
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: h}); err != nil {
		t.Fatal(err)
	}
	if s.checkContentCapability(wire.ServerInfo{}) == nil {
		t.Fatal("old Host accepted content tool")
	}
	if err := s.checkContentCapability(wire.ServerInfo{Capabilities: []string{"application-tool-result-content-v1"}}); err != nil {
		t.Fatal(err)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][contentTool.Name] {
		t.Fatal("callback format lost on restart")
	}
	if err := restored.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{defs: fixtureDefinitions("string")}}); err != nil {
		t.Fatal(err)
	}
	if err := restored.checkContentCapability(wire.ServerInfo{}); err != nil {
		t.Fatal("ordinary Bot now requires optional content capability")
	}
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][contentTool.Name] {
		t.Fatal("disabled experiment lost old callback receipt format")
	}
}
