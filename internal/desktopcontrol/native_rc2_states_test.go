package desktopcontrol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestPackagedHelperNativeRC2StatesAndScan(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_STATE_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_STATE_LOG")
	if helper == "" || title == "" || logPath == "" {
		t.Skip("set packaged helper, unique state fixture title and event log")
	}
	before, err := fixtureEvents(logPath)
	if err != nil || !before["ready:"+title] || before["selected:Acceptance row 80:true"] || before["selected:Acceptance row 80:false"] || before["expanded:false"] {
		t.Skip("state fixture is not in its fresh or confirmed expanded-only starting state")
	}
	alreadyExpanded := before["expanded:true"]
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-state-fixture"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" {
				_ = c.reconcile(ctx, id) // original receipt only; no input replay
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent)
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("states-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("state fixture title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("state fixture window unavailable")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("state app identity unavailable")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated rc.2 state fixture"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("state fixture authorization failed", out.StructuredContent)
	}
	// A small traversal with zero matches remains incomplete and resumable.
	scanArgs := map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role"}, "match": map[string]any{"within": window, "role": "row", "name_equals": "Acceptance row 80"}, "budget": map[string]any{"max_depth": 10, "max_visited_nodes": 8, "max_results": 1, "max_output_bytes": 3000, "read_deadline_ms": 3000}}
	var scan dw.Observation
	if err := protocol.Decode(call("states-scan-1", "observe", scanArgs), &scan); err != nil {
		t.Fatal(err)
	}
	if len(scan.Objects) != 0 || scan.Coverage.Complete || scan.Coverage.Continuation == "" {
		t.Fatal("zero-match scan was mistaken for complete absence")
	}
	scanArgs["continuation"] = scan.Coverage.Continuation
	if err := protocol.Decode(call("states-scan-2", "observe", scanArgs), &scan); err != nil {
		t.Fatal(err)
	}
	if len(scan.Objects) == 0 && scan.Coverage.Complete {
		t.Fatal("native continuation incorrectly proved target absent")
	}
	query := func(id, target string) dw.Ref {
		t.Helper()
		var ob dw.Observation
		args := map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": window, "name_equals": target}, "budget": map[string]any{"max_depth": 12, "max_visited_nodes": 512, "max_results": 3, "max_output_bytes": 8192, "read_deadline_ms": 5000}}
		if err := protocol.Decode(call(id, "observe", args), &ob); err != nil {
			t.Fatal(err)
		}
		if len(ob.Objects) != 1 {
			t.Fatalf("%s did not resolve uniquely; count=%d complete=%t", target, len(ob.Objects), ob.Coverage.Complete)
		}
		return ob.Objects[0].Ref
	}
	details := query("states-details", "Acceptance details")
	row := query("states-row", "Acceptance row 80")
	act := func(id string, step map[string]any) {
		t.Helper()
		var receipt dw.Receipt
		if err := protocol.Decode(call(id, "act", map[string]any{"steps": []map[string]any{step}}), &receipt); err != nil || receipt.Outcome != "completed" {
			t.Fatalf("%s did not complete: outcome=%s err=%v", id, receipt.Outcome, err)
		}
	}
	if !alreadyExpanded {
		act("states-expand", map[string]any{"id": "expand", "op": "set_expanded", "target": map[string]any{"ref": details}, "set_expanded": map[string]any{"expanded": true}, "completion": "verify"})
		// This custom AppKit row may lack a semantic scroll action. A typed
		// capability refusal with delivery:none is a provider limit, not input
		// uncertainty. Retain the exact original receipt and continue safely.
		raw, _ := json.Marshal(map[string]any{"requestId": "states-reveal", "args": map[string]any{"steps": []map[string]any{{"id": "reveal", "op": "scroll_into_view", "target": map[string]any{"ref": row}, "completion": "verify"}}}})
		out := c.CallTool(ctx, Prefix+"act", raw)
		if out.IsError {
			var receipt dw.Receipt
			if err := protocol.Decode(c.requests["states-reveal"].reply.Result, &receipt); err != nil || len(receipt.Steps) != 1 || receipt.Steps[0].Delivery != dw.DeliveryNone || receipt.Fault == nil || receipt.Fault.Code != "capability_unavailable" {
				_ = c.reconcile(ctx, "states-reveal")
				t.Fatal("semantic scroll outcome uncertain; no replay", out.StructuredContent)
			}
			t.Log("scroll_into_view: provider capability_unavailable, delivery none, original receipt retained")
		}
	} else {
		t.Log("resuming independently confirmed expanded=true; prior scroll_into_view was capability_unavailable with delivery none")
	}
	act("states-select", map[string]any{"id": "select", "op": "set_selected", "target": map[string]any{"ref": row}, "set_selected": map[string]any{"selected": true}, "completion": "verify"})
	act("states-deselect", map[string]any{"id": "deselect", "op": "set_selected", "target": map[string]any{"ref": row}, "set_selected": map[string]any{"selected": false}, "completion": "verify"})
	act("states-collapse", map[string]any{"id": "collapse", "op": "set_expanded", "target": map[string]any{"ref": details}, "set_expanded": map[string]any{"expanded": false}, "completion": "verify"})
	after, err := fixtureEvents(logPath)
	if err != nil || !after["expanded:true"] || !after["expanded:false"] || !after["selected:Acceptance row 80:true"] || !after["selected:Acceptance row 80:false"] {
		t.Fatal("independent state setter log lacks exact effects")
	}
	c.EndTurn("rc2-state-fixture")
	if out := c.reconcile(context.Background(), "states-select"); out.IsError {
		t.Fatal("original selected receipt lost after turn", out.StructuredContent)
	}
	t.Log("packaged helper: zero-match incomplete scan continuation, expanded true/false, selected true/false, independent setter events and post-turn receipt passed; semantic scroll capability is provider-dependent")
}

// Decode app-owned events rather than depending on JSON object key ordering.
func fixtureEvents(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	events := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var event struct {
			Event string `json:"event"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events[event.Event+":"+event.Value] = true
	}
	return events, scanner.Err()
}
