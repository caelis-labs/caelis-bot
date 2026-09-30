package caelis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	"image"
	"image/png"
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
	h := &acceptanceTools{defs: []api.ToolDefinition{desktopcontrol.Definitions()[0]}}
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
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][desktopcontrol.Definitions()[0].Name] {
		t.Fatal("callback format lost on restart")
	}
	if err := restored.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{defs: fixtureDefinitions("string")}}); err != nil {
		t.Fatal(err)
	}
	if err := restored.checkContentCapability(wire.ServerInfo{}); err != nil {
		t.Fatal("ordinary Bot now requires optional content capability")
	}
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][desktopcontrol.Definitions()[0].Name] {
		t.Fatal("disabled experiment lost old callback receipt format")
	}
}

// The public Host/provider test supplies synthetic pixels to isolate content-v1
// projection from native capture permission and UI state.
func desktopHostAcceptance(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel) {
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(pixels.Bytes())
	h := &acceptanceTools{defs: desktopcontrol.Definitions(), call: func(context.Context, string, json.RawMessage) api.ToolResult {
		return api.ToolResult{Content: []map[string]string{{"type": "image", "mimeType": "image/png", "data": encoded}}, StructuredContent: map[string]any{"tiles": []any{map[string]any{"asset": "fixture-image", "image_frame": "fixture-frame", "pixel_width": 4, "pixel_height": 4}}}}
	}}
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: h, ApprovedTools: desktopcontrol.ApprovedTools(), Instructions: "Synthetic Desktop World image transport acceptance"}); err != nil {
		t.Fatal(err)
	}
	current, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateConfiguration(ctx, "desktop-world-tools", string(current.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}); err != nil {
		t.Fatal(err)
	}
	model.set("CASE_DESKTOP_CAPTURE", modelStep{Name: "bot_desktop_capture", Args: map[string]any{"requestId": "capture-fixture", "args": map[string]any{"kind": "visible_region", "target": "fixture-window"}}}, modelStep{Reply: "DESKTOP_IMAGE_RECEIVED"})
	submitAcceptance(t, ctx, s, "CASE_DESKTOP_CAPTURE")
	requests := model.seen("CASE_DESKTOP_CAPTURE")
	if len(requests) != 2 {
		t.Fatal("missing capture continuation")
	}
	raw, _ := json.Marshal(requests[1])
	body := string(raw)
	if !strings.Contains(body, "data:image/png;base64,"+encoded) || !strings.Contains(body, "fixture-frame") {
		t.Fatal("lost pixels or transform metadata")
	}
	if !strings.Contains(body, `"type":"image_url"`) && !strings.Contains(body, `"type":"input_image"`) {
		t.Fatal("capture downgraded to text")
	}
	t.Log("Desktop World tool schema -> real Host content-v1 -> provider image and metadata passed; synthetic pixels, no native UI claim")
}
