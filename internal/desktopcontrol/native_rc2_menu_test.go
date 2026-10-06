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

func TestPackagedHelperNativeRC2Menu(t *testing.T) {
	helper, title, logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_MENU_TITLE"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_MENU_LOG")
	if helper == "" || title == "" || logPath == "" {
		t.Skip("requires isolated menu fixture and packaged helper")
	}
	events, err := fixtureEvents(logPath)
	if err != nil || !events["ready:"+title] {
		t.Fatal("menu fixture not ready")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-menu"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" && c.requests[id] != nil {
				t.Logf("original menu receipt: %s", c.requests[id].reply.Result)
				_ = c.reconcile(ctx, id)
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("menu-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if app != "" {
				t.Fatal("menu fixture title ambiguous")
			}
			app = o.App
		}
	}
	if app == "" {
		t.Fatal("menu fixture not observed")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("menu app identity missing")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated menu acceptance"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("menu grant refused", out.StructuredContent)
	}
	var observed dw.Observation
	if err := protocol.Decode(call("menu-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{app}}, "projection": "outline", "fields": []string{"name", "role", "capabilities", "states"}, "match": map[string]any{"within": app, "name_equals": "Increment Fixture Counter"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}}), &observed); err != nil {
		t.Fatal(err)
	}
	var item dw.Ref
	var cap string
	for _, o := range observed.Objects {
		if o.Role == "menu" || o.Role == "menu_bar" || o.Role == "menu_item" {
			if o.Name.Value != nil {
				t.Logf("menu AX role=%s name=%s", o.Role, *o.Name.Value)
			}
		}
		if o.Name.Value != nil && *o.Name.Value == "Increment Fixture Counter" {
			item = o.Ref
			t.Logf("menu target states=%v", o.States)
			for _, c := range o.Capabilities {
				if c.Name == "invoke" {
					cap = c.Support + "/" + c.Availability
				}
			}
		}
	}
	t.Logf("menu target observed=%t invoke=%s coverage_complete=%t", item != "", cap, observed.Coverage.Complete)
	if os.Getenv("CAELIS_BOT_TEST_DESKTOP_MENU_ACT") != "1" {
		return
	}
	if item == "" || cap != "supported/available" {
		t.Fatal("menu item not explicitly actionable; no input sent")
	}
	once := os.Getenv("CAELIS_BOT_TEST_DESKTOP_MENU_ONCE")
	if once == "" {
		t.Fatal("menu once marker required")
	}
	f, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("menu input already attempted", err)
	}
	_, writeErr := f.WriteString(title + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("menu marker failed", writeErr, closeErr)
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("menu-invoke-dispatch-once", "act", map[string]any{"steps": []map[string]any{{"id": "invoke-menu", "op": "invoke", "target": map[string]any{"ref": item}, "completion": "dispatch"}}}), &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatalf("menu invoke uncertain: outcome=%s err=%v", receipt.Outcome, err)
	}
	after, err := fixtureEvents(logPath)
	if err != nil || !after["menu_count:1"] {
		t.Fatal("app-owned menu effect absent")
	}
	c.EndTurn("rc2-menu")
	if out := c.reconcile(context.Background(), "menu-invoke-dispatch-once"); out.IsError {
		t.Fatal("original menu receipt lost", out.StructuredContent)
	}
	t.Log("packaged helper menu invoke and independent app-owned counter passed")
}
