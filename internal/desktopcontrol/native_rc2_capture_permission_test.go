package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

// This diagnostic never requests or changes macOS permission. It only asks the
// published managed helper for an isolated window-content capture and records
// whether the real OS gate allowed it. Image bytes stay in the ignored cache.
func TestPackagedHelperNativeRC2CapturePermission(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	if helper == "" || os.Getenv("CAELIS_BOT_TEST_DESKTOP_CAPTURE") != "1" {
		t.Skip("explicit published-helper capture diagnostic only")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-capture-permission"), 30*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) (json.RawMessage, bool) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			t.Logf("%s rejected code=%v", id, out.StructuredContent["error"])
			return nil, false
		}
		return c.requests[id].reply.Result, true
	}
	raw, ok := call("capture-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if !ok {
		t.Fatal("capture inventory failed")
	}
	var inventory dw.Observation
	if err := protocol.Decode(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	var app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == "RC2 Electron Synthetic Document" {
			if app != "" {
				t.Fatal("capture fixture ambiguous")
			}
			app = o.App
		}
	}
	if app == "" {
		t.Fatal("isolated capture fixture absent")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("capture fixture app identity absent")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated window capture permission check"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("capture app grant refused", out.StructuredContent)
	}
	raw, ok = call("capture-windows", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{app}}, "projection": "capture_windows", "freshness": map[string]any{"mode": "refresh"}, "fields": []string{"name", "role", "capabilities", "app"}, "budget": map[string]any{"max_results": 32, "max_output_bytes": 16384, "read_deadline_ms": 5000}})
	if !ok {
		t.Log("real capture-window enumeration blocked before pixels; no TCC change")
		return
	}
	var observed dw.Observation
	if err := protocol.Decode(raw, &observed); err != nil {
		t.Fatal(err)
	}
	var target dw.Ref
	for _, o := range observed.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == "RC2 Electron Synthetic Document" {
			target = o.Ref
		}
	}
	if target == "" {
		t.Logf("capture window absent: count=%d complete=%t; no pixels requested", len(observed.Objects), observed.Coverage.Complete)
		return
	}
	raw, ok = call("capture-once", "capture", map[string]any{"kind": "window_content", "target": target, "max_pixel_width": 640, "max_pixel_height": 480})
	if !ok {
		t.Log("real published-helper capture blocked by OS or provider; no TCC change")
		return
	}
	var result dw.CaptureResult
	if err := protocol.Decode(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tiles) != 1 {
		t.Fatal("capture succeeded without exactly one window tile")
	}
	t.Logf("isolated real window capture succeeded: tiles=%d width=%d height=%d", len(result.Tiles), result.Tiles[0].PixelWidth, result.Tiles[0].PixelHeight)
}
