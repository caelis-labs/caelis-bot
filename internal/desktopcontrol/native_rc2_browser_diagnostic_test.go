package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

// This diagnostic uses a new page and checkbox, never the earlier unknown
// Chrome input target. Its once marker is created before any input request.
func TestPackagedHelperNativeRC2BrowserDiagnostic(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_DIAG_TITLE")
	oncePath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_DIAG_ONCE")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_BROWSER_DIAG_EVIDENCE")
	if helper == "" || title == "" || oncePath == "" || evidencePath == "" {
		t.Skip("requires packaged helper, exact new diagnostic page, once marker and evidence path")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-browser-diagnostic"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) (json.RawMessage, bool) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			return nil, false
		}
		return c.requests[id].reply.Result, true
	}
	raw, ok := call("diag-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if !ok {
		t.Fatal("diagnostic inventory failed before input")
	}
	var inventory dw.Observation
	if err := protocol.Decode(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("diagnostic title is ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("diagnostic window unavailable")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			appName = *o.Name.Value
		}
	}
	if appName == "" {
		t.Fatal("diagnostic app identity unavailable")
	}
	h, ok := c.client.(*host.Client)
	if !ok || h.Hello.InputMode != dw.InputModeCooperative || h.Hello.Environment.InputMode != dw.InputModeCooperative || h.Hello.Protocol != "desktop-world/helper-v0.1" {
		t.Fatal("actual helper hello/input mode mismatch")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": appName, "purpose": "new isolated checkbox diagnostic"})
	if result := c.CallTool(ctx, Prefix+"authorize", auth); result.IsError {
		t.Fatal("diagnostic app grant refused", result.StructuredContent["error"])
	}
	grants, err := h.Grants(ctx, "rc2-browser-diagnostic")
	if err != nil || len(grants.Grants) != 1 || grants.Grants[0].State != "active" {
		t.Fatal("diagnostic grant not active", err)
	}
	raw, ok = call("diag-window-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role"}, "budget": map[string]any{"max_depth": 14, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if !ok {
		t.Fatal("diagnostic window outline failed before input")
	}
	var outline dw.Observation
	if err := protocol.Decode(raw, &outline); err != nil {
		t.Fatal(err)
	}
	var document dw.Ref
	for _, o := range outline.Objects {
		if o.Role == "document" && o.Name.Value != nil && *o.Name.Value == title {
			document = o.Ref
		}
	}
	if document == "" {
		t.Fatal("diagnostic document not observed")
	}
	raw, ok = call("diag-checkbox-observe", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{document}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": document, "role": "checkbox", "name_equals": "Diagnostic checkbox"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}})
	if !ok {
		t.Fatal("diagnostic checkbox observation failed before input")
	}
	var check dw.Observation
	if err := protocol.Decode(raw, &check); err != nil || len(check.Objects) != 1 || !check.Coverage.Complete {
		t.Fatalf("diagnostic checkbox not proven unique: count=%d complete=%t err=%v", len(check.Objects), check.Coverage.Complete, err)
	}
	target := check.Objects[0]
	checked := target.States["checked"]
	if checked.Status != dw.FactKnown || checked.Value == nil || *checked.Value {
		t.Fatal("diagnostic checkbox has no known false baseline")
	}
	var support string
	for _, capability := range target.Capabilities {
		if capability.Name == "set_checked" {
			support = capability.Support + "/" + capability.Availability
		}
	}
	if support != "supported/available" {
		t.Fatal("provider did not advertise executable set_checked", support)
	}
	marker, err := os.OpenFile(oncePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("diagnostic input already attempted or marker unavailable", err)
	}
	if _, err := marker.WriteString(title + "\n"); err != nil {
		marker.Close()
		t.Fatal("diagnostic marker write failed", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal("diagnostic marker close failed", err)
	}
	actRaw, _ := json.Marshal(map[string]any{"requestId": "diag-semantic-once", "args": map[string]any{"steps": []map[string]any{{"id": "set", "op": "set_checked", "target": map[string]any{"ref": target.Ref}, "set_checked": map[string]any{"checked": true}, "completion": "verify"}}}})
	result := c.CallTool(ctx, Prefix+"act", actRaw)
	request := c.requests["diag-semantic-once"]
	if request == nil {
		t.Fatal("diagnostic input was not registered; inspect marker before any new task")
	}
	var receipt dw.Receipt
	if err := protocol.Decode(request.reply.Result, &receipt); err != nil {
		t.Fatal("original diagnostic receipt missing or malformed; no replay", err)
	}
	if len(receipt.Steps) != 1 {
		t.Fatal("original diagnostic receipt has no single step; no replay")
	}
	reconciled := c.reconcile(ctx, "diag-semantic-once")
	// A failed native action is still a valid original result. Reconcile keeps
	// its error status and the same receipt; IsError alone is not missing data.
	evidence := map[string]any{"helper_protocol": h.Hello.Protocol, "hello_input_mode": h.Hello.InputMode, "environment_input_mode": h.Hello.Environment.InputMode, "input_policy": h.Hello.InputPolicy, "app_name": appName, "grant_state": grants.Grants[0].State, "target_role": target.Role, "target_name": *target.Name.Value, "checked_before": false, "set_checked_capability": support, "result_is_error": result.IsError, "reconcile_error": reconciled.StructuredContent["error"], "receipt": receipt}
	b, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(evidencePath, b, 0600); err != nil {
		t.Fatal("could not save classified original receipt", err)
	}
	t.Logf("original browser diagnostic outcome=%s step=%s delivery=%s verification=%s fault=%v; no replay", receipt.Outcome, receipt.Steps[0].State, receipt.Steps[0].Delivery, receipt.Steps[0].Verification, receipt.Fault)
}
