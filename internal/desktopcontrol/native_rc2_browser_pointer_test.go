package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestPackagedHelperNativeRC2BrowserPointer(t *testing.T) {
	helper, once := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_POINTER_ONCE")
	if helper == "" || once == "" {
		t.Skip("requires exact fresh synthetic Chrome pointer page and once marker")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-browser-pointer"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" && c.requests[id] != nil {
				t.Logf("original browser pointer receipt: %s", c.requests[id].reply.Result)
				_ = c.reconcile(ctx, id)
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("browser-pointer-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	const title = "RC2 Browser Pointer Diagnostic"
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && strings.Contains(*o.Name.Value, "RC2 Browser") {
			t.Logf("isolated browser candidate title=%s", *o.Name.Value)
		}
		if o.Kind == dw.KindWindow && o.Name.Value != nil && strings.HasPrefix(*o.Name.Value, title+" - Google Chrome") {
			if window != "" {
				t.Fatal("pointer page title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated pointer page unavailable")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			appName = *o.Name.Value
		}
	}
	if appName == "" {
		t.Fatal("Chrome app name unavailable")
	}
	h, ok := c.client.(*host.Client)
	if !ok || h.Hello.InputMode != dw.InputModeCooperative || h.Hello.InputPolicy != dw.InputShared {
		t.Fatal("actual helper cooperative mode mismatch")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": appName, "purpose": "independent synthetic pointer-only page"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("pointer page grant refused", out.StructuredContent)
	}
	var outline dw.Observation
	if err := protocol.Decode(call("browser-pointer-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role"}, "budget": map[string]any{"max_depth": 14, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &outline); err != nil {
		t.Fatal(err)
	}
	var document dw.Ref
	for _, o := range outline.Objects {
		if o.Role == "document" && o.Name.Value != nil && *o.Name.Value == title {
			document = o.Ref
		}
	}
	if document == "" {
		t.Fatal("pointer document unavailable")
	}
	var observed dw.Observation
	if err := protocol.Decode(call("browser-pointer-button", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{document}}, "projection": "outline", "fields": []string{"name", "role", "states", "bounds", "capabilities"}, "match": map[string]any{"within": document, "name_equals": "Pointer acceptance button"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}}), &observed); err != nil {
		t.Fatal(err)
	}
	if !observed.Coverage.Complete || len(observed.Objects) != 1 {
		t.Fatal("pointer button not proven unique")
	}
	button := observed.Objects[0]
	// Physical pointer actions are gated by observed geometry and the input
	// transaction, not advertised as per-object semantic capabilities.
	enabled, offscreen := button.States["enabled"], button.States["offscreen"]
	t.Logf("Chrome pointer button role=%s enabled=%s offscreen=%s bounds=%s", button.Role, enabled.Status, offscreen.Status, button.Bounds.Status)
	if button.Role != "button" || enabled.Status != dw.FactKnown || enabled.Value == nil || !*enabled.Value || offscreen.Status != dw.FactKnown || offscreen.Value == nil || *offscreen.Value || button.Bounds.Status != dw.FactKnown || button.Bounds.Value == nil || button.Bounds.Value.Rect.Width <= 0 || button.Bounds.Value.Rect.Height <= 0 {
		t.Fatal("pointer target lacks proven visible hittable geometry; no input sent")
	}
	marker, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("pointer scenario already attempted", err)
	}
	_, writeErr := marker.WriteString(title + "\n")
	closeErr := marker.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("pointer marker failed", writeErr, closeErr)
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("browser-pointer-click-once", "act", map[string]any{"steps": []map[string]any{{"id": "click", "op": "pointer.click", "target": map[string]any{"ref": button.Ref}, "click": map[string]any{"button": "left", "count": 1}, "completion": "dispatch"}}}), &receipt); err != nil {
		t.Fatal("browser pointer receipt malformed", err)
	}
	t.Logf("Chrome pointer original outcome=%s steps=%d", receipt.Outcome, len(receipt.Steps))
	if receipt.Outcome != "completed" {
		t.Fatal("Chrome pointer click uncertain; no replay")
	}
	c.EndTurn("rc2-browser-pointer")
	if out := c.reconcile(context.Background(), "browser-pointer-click-once"); out.IsError {
		t.Fatal("original pointer receipt lost", out.StructuredContent)
	}
}
