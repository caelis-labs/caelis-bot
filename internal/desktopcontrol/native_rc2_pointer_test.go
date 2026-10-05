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

func TestPackagedHelperNativeRC2PointerAndBindings(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_POINTER_TITLE")
	onceFile := os.Getenv("CAELIS_BOT_TEST_DESKTOP_POINTER_ONCE_FILE")
	if helper == "" || title == "" || onceFile == "" {
		t.Skip("set packaged helper, exact isolated pointer fixture title and new once-file")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-pointer-fixture"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" {
				var receipt dw.Receipt
				if request := c.requests[id]; request != nil && protocol.Decode(request.reply.Result, &receipt) == nil {
					t.Logf("original %s outcome=%s seat=%s steps=%v", id, receipt.Outcome, receipt.SeatHealth, receipt.Steps)
				}
				_ = c.reconcile(ctx, id) // original receipt only; never replay
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("pointer-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("isolated pointer fixture title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated pointer fixture window unavailable")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("pointer fixture app identity unavailable")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated rc.2 pointer fixture"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("pointer fixture authorization failed", out.StructuredContent["error"])
	}
	var outline dw.Observation
	if err := protocol.Decode(call("pointer-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "budget": map[string]any{"max_depth": 10, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &outline); err != nil {
		t.Fatal(err)
	}
	var slider dw.Ref
	for _, o := range outline.Objects {
		if o.Name.Value != nil && *o.Name.Value == "Verification slider" {
			slider = o.Ref
		}
	}
	if slider == "" {
		t.Fatal("isolated slider unavailable")
	}
	var bound dw.Receipt
	if err := protocol.Decode(call("pointer-bind-wait", "act", map[string]any{"steps": []map[string]any{
		{"id": "bind", "op": "bind", "bind": map[string]any{"name": "filter", "locator": map[string]any{"within": window, "kind": "ui", "role": "checkbox", "name_equals": "Only incomplete"}, "require_unique": true}},
		{"id": "wait", "op": "wait", "after": []map[string]any{{"target": map[string]any{"bound": "filter"}, "property": "checked", "equals_bool": false}}},
	}}), &bound); err != nil || bound.Outcome != "completed" {
		t.Fatalf("bind/wait did not complete: outcome=%s err=%v", bound.Outcome, err)
	}
	marker, err := os.OpenFile(onceFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("pointer input already attempted or once-file unavailable", err)
	}
	if _, err := marker.WriteString(title + "\n"); err != nil {
		marker.Close()
		t.Fatal("pointer once-file write failed", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal("pointer once-file close failed", err)
	}
	start := map[string]any{"anchor": map[string]any{"target": slider, "u": 0.2, "v": 0.5}}
	end := map[string]any{"anchor": map[string]any{"target": slider, "u": 0.8, "v": 0.5}}
	var moved dw.Receipt
	if err := protocol.Decode(call("pointer-move-drag", "act", map[string]any{"steps": []map[string]any{
		{"id": "move", "op": "pointer.move", "target": start},
		{"id": "drag", "op": "pointer.drag", "target": start, "drag": map[string]any{"to": end, "duration_ms": 200}},
	}}), &moved); err != nil || moved.Outcome != "completed" {
		t.Fatalf("pointer move/drag did not complete: outcome=%s err=%v", moved.Outcome, err)
	}
	c.EndTurn("rc2-pointer-fixture")
	if out := c.reconcile(context.Background(), "pointer-move-drag"); out.IsError {
		t.Fatal("original pointer receipt lost after turn", out.StructuredContent["error"])
	}
	t.Log("packaged helper: exact native bind/wait and one cooperative pointer move/drag completed; independently inspect slider")
}
