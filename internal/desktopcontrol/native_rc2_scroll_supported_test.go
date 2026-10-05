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

func TestPackagedHelperNativeRC2SupportedScroll(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_SCROLL_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_SCROLL_LOG")
	if helper == "" || title == "" || logPath == "" {
		t.Skip("set published helper and isolated supported-scroll fixture")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-supported-scroll"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" {
				if request := c.requests[id]; request != nil {
					t.Logf("original act receipt: %s", request.reply.Result)
				}
				reconciled := c.reconcile(ctx, id) // original receipt only
				t.Logf("same-ID reconcile error=%t result=%v", reconciled.IsError, reconciled.StructuredContent)
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	before, err := fixtureEvents(logPath)
	if err != nil || !before["ready:"+title] {
		t.Fatal("scroll fixture not ready")
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("scroll-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("scroll fixture window ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("scroll fixture window unavailable")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			appName = *o.Name.Value
		}
	}
	if appName == "" {
		t.Fatal("scroll app name unavailable")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": appName, "purpose": "isolated supported-scroll test"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("scroll app grant failed", out.StructuredContent["error"])
	}
	query := func(id string) dw.Observation {
		t.Helper()
		var observed dw.Observation
		if err := protocol.Decode(call(id, "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": window, "name_equals": "Distant acceptance button"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}}), &observed); err != nil {
			t.Fatal(err)
		}
		return observed
	}
	observed := query("scroll-target-before")
	if len(observed.Objects) != 1 || !observed.Coverage.Complete {
		t.Fatalf("scroll target not proven unique: count=%d complete=%t continuation=%t", len(observed.Objects), observed.Coverage.Complete, observed.Coverage.Continuation != "")
	}
	target := observed.Objects[0]
	var support string
	for _, cap := range target.Capabilities {
		if cap.Name == "scroll_into_view" {
			support = cap.Support + "/" + cap.Availability
		}
	}
	offscreen := target.States["offscreen"]
	t.Logf("native WebKit target role=%s scroll=%s offscreen_status=%s offscreen_true=%t", target.Role, support, offscreen.Status, offscreen.Value != nil && *offscreen.Value)
	if os.Getenv("CAELIS_BOT_TEST_DESKTOP_SCROLL_ACT") != "1" {
		return
	}
	if support != "supported/available" || offscreen.Status != dw.FactKnown || offscreen.Value == nil || !*offscreen.Value {
		t.Fatal("provider did not advertise a known offscreen scroll target; no input sent")
	}
	oncePath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_SCROLL_ONCE")
	if oncePath == "" {
		t.Fatal("set a fresh scroll once-file path")
	}
	marker, err := os.OpenFile(oncePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("scroll action already attempted or marker unavailable", err)
	}
	_, writeErr := marker.WriteString(title + "\n")
	closeErr := marker.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("scroll marker could not be written", writeErr, closeErr)
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("scroll-supported-once", "act", map[string]any{"steps": []map[string]any{{"id": "reveal", "op": "scroll_into_view", "target": map[string]any{"ref": target.Ref}, "completion": "verify"}}}), &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatalf("native supported scroll did not complete: outcome=%s err=%v", receipt.Outcome, err)
	}
	for i := 0; i < 30; i++ {
		events, err := fixtureEvents(logPath)
		if err == nil && events["scrolled:true"] {
			break
		}
		if i == 29 {
			t.Fatal("independent WebKit scroll callback absent")
		}
		time.Sleep(50 * time.Millisecond)
	}
	after := query("scroll-target-after")
	if len(after.Objects) != 1 || after.Objects[0].States["offscreen"].Status != dw.FactKnown || after.Objects[0].States["offscreen"].Value == nil || *after.Objects[0].States["offscreen"].Value {
		t.Fatal("native target remained offscreen after supported action")
	}
	c.EndTurn("rc2-supported-scroll")
	if out := c.reconcile(context.Background(), "scroll-supported-once"); out.IsError {
		t.Fatal("original scroll receipt unavailable after turn", out.StructuredContent["error"])
	}
	t.Log("packaged helper: supported AXScrollToVisible moved the real offscreen WebKit button into view; app-owned callback and original receipt passed")
}
