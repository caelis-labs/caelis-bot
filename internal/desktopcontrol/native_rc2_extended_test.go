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

// Continue a known, completed fixture state. This deliberately uses fresh
// request IDs and a new helper epoch; it never tries to recover an old run from
// another process. Only the prior independently confirmed single submission
// qualifies the starting state.
func TestPackagedHelperNativeRC2Extended(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FIXTURE_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FIXTURE_LOG")
	if helper == "" || title == "" || logPath == "" {
		t.Skip("set packaged helper, unique fixture title, and fixture log")
	}
	readResult := func() (int, string) {
		t.Helper()
		b, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal("fixture log unavailable", err)
		}
		var r struct {
			Submissions int
			Text        string
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal("fixture log invalid", err)
		}
		return r.Submissions, r.Text
	}
	if n, v := readResult(); n != 1 || v != "Bot rc.2 native 🌍" {
		t.Skip("original native fixture state is not the independently confirmed single submission")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-extended-fixture"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) (json.RawMessage, bool) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" {
				// Query only the original receipt before abandoning this session.
				recovered := c.reconcile(ctx, id)
				t.Logf("original %s receipt error=%v recovery_error=%v", id, out.StructuredContent["error"], recovered.StructuredContent["error"])
			}
			return nil, false
		}
		return c.requests[id].reply.Result, true
	}
	raw, ok := call("rc2-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if !ok {
		t.Fatal("fixture inventory failed before input")
	}
	var inventory dw.Observation
	if err := protocol.Decode(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	var window dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("fixture title ambiguous")
			}
			window = o.Ref
		}
	}
	if window == "" {
		t.Fatal("fixture window not observed")
	}
	// The reviewed selector is an exact window title. It may become active only
	// after the helper independently proves one complete matching app instance.
	declare, _ := json.Marshal(map[string]string{"operation": "declare", "windowTitle": title, "purpose": "isolated rc.2 fixture task"})
	if out := c.CallTool(ctx, Prefix+"authorize", declare); out.IsError {
		t.Fatal("exact future-app declaration failed", out.StructuredContent)
	}
	status, err := c.client.Grants(ctx, "rc2-extended-fixture")
	if err != nil || len(status.Grants) != 1 || status.Grants[0].State != "active" {
		t.Fatal("declaration did not bind exactly one current fixture", status, err)
	}
	grantID := status.Grants[0].ID
	raw, ok = call("rc2-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities", "value_preview"}, "budget": map[string]any{"max_depth": 10, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if !ok {
		t.Fatal("fixture outline failed before input")
	}
	var outline dw.Observation
	if err := protocol.Decode(raw, &outline); err != nil {
		t.Fatal(err)
	}
	var field, checkbox, submit, scroll dw.Ref
	for _, o := range outline.Objects {
		if o.Name.Value == nil {
			continue
		}
		switch *o.Name.Value {
		case "Verification text":
			field = o.Ref
		case "Only incomplete":
			checkbox = o.Ref
		case "Submit once":
			submit = o.Ref
		case "Verification scroll area":
			scroll = o.Ref
		}
	}
	if field == "" || checkbox == "" || submit == "" {
		t.Fatal("fixture controls unavailable")
	}
	if _, ok := call("rc2-read", "read", map[string]any{"target": field, "limit_runes": 128}); !ok {
		t.Fatal("bounded text read failed")
	}
	value := "Bot rc.2 keyboard 🌙"
	plan := []map[string]any{
		{"id": "focus-field", "op": "focus", "target": map[string]any{"ref": field}},
		{"id": "bind-current-focus", "op": "bind_focus", "bind_focus": map[string]any{"name": "input", "within": window}},
		{"id": "select-all", "op": "keyboard.press", "target": map[string]any{"bound": "input"}, "press": map[string]any{"key": "A", "modifiers": []string{"primary"}}},
		{"id": "type", "op": "keyboard.type_text", "target": map[string]any{"bound": "input"}, "type_text": map[string]any{"text": value}},
		{"id": "submit", "op": "pointer.click", "target": map[string]any{"ref": submit}, "click": map[string]any{"button": "left", "count": 1}},
	}
	raw, ok = call("rc2-keyboard-once", "act", map[string]any{"steps": plan})
	if !ok {
		t.Fatal("keyboard plan uncertain; original receipt queried, no replay")
	}
	var receipt dw.Receipt
	if err := protocol.Decode(raw, &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatal("keyboard plan did not complete", receipt.Outcome, err)
	}
	for i := 0; i < 30; i++ {
		if n, v := readResult(); n == 2 && v == value {
			break
		}
		if i == 29 {
			t.Fatal("independent keyboard/pointer business readback failed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A fresh cursor read does not send input and keeps the original window view.
	if outline.Cursor != "" {
		if _, ok := call("rc2-delta", "sync", map[string]any{"cursor": outline.Cursor, "max_output_bytes": 8192}); !ok {
			t.Fatal("delta read failed")
		}
	}
	if _, ok := call("rc2-uncheck", "act", map[string]any{"steps": []map[string]any{{"id": "uncheck", "op": "set_checked", "target": map[string]any{"ref": checkbox}, "set_checked": map[string]any{"checked": false}, "completion": "verify"}}}); !ok {
		t.Fatal("semantic false uncertain; original receipt queried")
	}
	if scroll != "" {
		// A known scroll region is the only pointer target. Failure stops the
		// test; it is never retried under a new request ID.
		if _, ok := call("rc2-scroll", "act", map[string]any{"steps": []map[string]any{{"id": "scroll", "op": "pointer.scroll", "target": map[string]any{"ref": scroll}, "scroll": map[string]any{"unit": "wheel_step", "dy": 1}}}}); !ok {
			t.Fatal("pointer scroll uncertain; original receipt queried")
		}
	}
	revoke, _ := json.Marshal(map[string]string{"operation": "revoke", "grantId": grantID, "purpose": "fixture task complete"})
	if out := c.CallTool(ctx, Prefix+"authorize", revoke); out.IsError {
		t.Fatal("dynamic revocation failed", out.StructuredContent)
	}
	denied, _ := json.Marshal(map[string]any{"requestId": "rc2-after-revoke", "args": map[string]any{"steps": []map[string]any{{"id": "submit", "op": "invoke", "target": map[string]any{"ref": submit}}}}})
	if out := c.CallTool(ctx, Prefix+"act", denied); !out.IsError {
		t.Fatal("input admitted after revocation")
	}
	if n, v := readResult(); n != 2 || v != value {
		t.Fatal("revoked input changed fixture business result")
	}
	c.EndTurn("rc2-extended-fixture")
	if got := c.reconcile(context.Background(), "rc2-keyboard-once"); got.IsError {
		t.Fatal("completed original keyboard receipt lost after turn", got.StructuredContent)
	}
	t.Log("packaged helper: exact dynamic window grant, bounded read/delta, one bind_focus keyboard+pointer plan, semantic false, pointer scroll, revoke/refusal, independent two-submit result and post-turn receipt passed")
}
