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

func TestPackagedHelperNativeRC2Browser(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_TITLE")
	if helper == "" || title == "" {
		t.Skip("set packaged helper and exact isolated browser fixture title")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-browser-fixture"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" {
				if request := c.requests[id]; request != nil {
					var receipt dw.Receipt
					if protocol.Decode(request.reply.Result, &receipt) == nil {
						t.Logf("original receipt outcome=%s seat=%s steps=%v", receipt.Outcome, receipt.SeatHealth, receipt.Steps)
					}
				}
				recovered := c.reconcile(ctx, id) // original receipt only; no replay
				t.Logf("original receipt recovery_error=%t", recovered.IsError)
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("browser-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("isolated browser fixture title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated browser fixture window unavailable")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("browser app identity unavailable")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated local synthetic page"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("browser authorization failed", out.StructuredContent["error"])
	}
	var outline dw.Observation
	if err := protocol.Decode(call("browser-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities", "value_preview"}, "budget": map[string]any{"max_depth": 12, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &outline); err != nil {
		t.Fatal(err)
	}
	var document dw.Ref
	for _, o := range outline.Objects {
		if o.Role == "document" && o.Name.Value != nil && *o.Name.Value == title {
			document = o.Ref
		}
	}
	if document == "" {
		t.Fatal("synthetic browser document unavailable")
	}
	var page dw.Observation
	if err := protocol.Decode(call("browser-page", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{document}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities", "value_preview"}, "budget": map[string]any{"max_depth": 16, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &page); err != nil {
		t.Fatal(err)
	}
	var checkbox, increment dw.Ref
	for _, o := range page.Objects {
		if o.Name.Value == nil {
			continue
		}
		switch *o.Name.Value {
		case "Only incomplete":
			if o.Role == "checkbox" {
				checkbox = o.Ref
			}
		case "Increment":
			if o.Role == "button" {
				increment = o.Ref
			}
		}
	}
	if checkbox == "" || increment == "" {
		t.Fatal("synthetic browser controls unavailable")
	}
	if os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_ACT") != "1" {
		t.Logf("packaged helper: exact isolated browser window, document and controls discovered; outer/inner coverage complete=%t/%t; no input requested", outline.Coverage.Complete, page.Coverage.Complete)
		return
	}
	// A browser attempt can have uncertain delivery even if the page's current
	// state is unchanged. Require a durable once marker before sending input.
	onceFile := os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_ONCE_FILE")
	if onceFile == "" {
		t.Fatal("set a new isolated once-file path before browser input")
	}
	marker, err := os.OpenFile(onceFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("browser input already attempted or once-file unavailable", err)
	}
	if _, err := marker.WriteString(title + "\n"); err != nil {
		marker.Close()
		t.Fatal("browser once-file write failed", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal("browser once-file close failed", err)
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("browser-semantic-once", "act", map[string]any{"steps": []map[string]any{
		{"id": "check", "op": "set_checked", "target": map[string]any{"ref": checkbox}, "set_checked": map[string]any{"checked": true}, "completion": "verify"},
		{"id": "increment", "op": "invoke", "target": map[string]any{"ref": increment}},
	}}), &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatalf("browser action did not complete; outcome=%s err=%v", receipt.Outcome, err)
	}
	c.EndTurn("rc2-browser-fixture")
	if out := c.reconcile(context.Background(), "browser-semantic-once"); out.IsError {
		t.Fatal("original browser receipt lost after turn", out.StructuredContent["error"])
	}
	t.Log("packaged helper: isolated Chrome checkbox true and button invoke completed; independently check the page DOM")
}
