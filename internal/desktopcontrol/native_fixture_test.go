package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

// Opt-in against one uniquely titled, disposable AppKit fixture. The fixture
// writes its own result file, independent of AX readback and helper receipts.
// This test never launches or grants the user's normal applications.
func TestPackagedHelperNativeFixture(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FIXTURE_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_FIXTURE_LOG")
	if helper == "" || title == "" || logPath == "" {
		t.Skip("set packaged helper, unique fixture title, and fixture log")
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("fixture result path must be new and isolated")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "native-rc2-fixture"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			t.Fatalf("%s failed: %v", op, out.StructuredContent)
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("fixture-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Status == dw.FactKnown && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("fixture window title is ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("unique fixture window absent or incomplete")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Kind == dw.KindApplication && o.Name.Value != nil {
			appName = *o.Name.Value
		}
	}
	if appName == "" {
		t.Fatal("fixture application identity unavailable")
	}
	authorize, _ := json.Marshal(map[string]string{"application": string(app), "name": appName, "purpose": "user-authorized isolated rc.2 fixture"})
	if out := c.CallTool(ctx, Prefix+"authorize", authorize); out.IsError {
		t.Fatal("fixture grant refused", out.StructuredContent)
	}
	var outline dw.Observation
	if err := protocol.Decode(call("fixture-outline", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities", "value_preview"}, "budget": map[string]any{"max_depth": 10, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &outline); err != nil {
		t.Fatal(err)
	}
	var field, checkbox, submit dw.Ref
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
		}
	}
	if field == "" || checkbox == "" || submit == "" {
		t.Fatal("fixture controls absent")
	}
	value := "Bot rc.2 native 🌍"
	steps := []map[string]any{
		{"id": "check", "op": "set_checked", "target": map[string]any{"ref": checkbox}, "set_checked": map[string]any{"checked": true}, "completion": "verify"},
		{"id": "value", "op": "set_value", "target": map[string]any{"ref": field}, "set_value": map[string]any{"text": value}, "completion": "verify"},
		{"id": "submit", "op": "invoke", "target": map[string]any{"ref": submit}, "completion": "dispatch"},
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("fixture-semantic-once", "act", map[string]any{"steps": steps}), &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatal("semantic plan failed", receipt.Outcome, err)
	}
	for i := 0; i < 30; i++ {
		data, err := os.ReadFile(logPath)
		var result struct {
			Submissions int
			Text        string
		}
		if err == nil && json.Unmarshal(data, &result) == nil && result.Submissions == 1 && result.Text == value {
			break
		}
		if i == 29 {
			t.Fatal("independent fixture submit/text readback failed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Identical ID/body retrieves the original receipt without another submit.
	call("fixture-semantic-once", "act", map[string]any{"steps": steps})
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), `"submissions":1`) {
		t.Fatal("duplicate request delivered another submit")
	}
	c.EndTurn("native-rc2-fixture")
	if got := c.reconcile(context.Background(), "fixture-semantic-once"); got.IsError || got.StructuredContent == nil {
		t.Fatal("original receipt unavailable after turn", got)
	}
	t.Log("packaged helper: exact fixture discovery/grant, semantic checkbox/value/invoke, independent one-submit log, duplicate suppression, and post-turn receipt passed")
}
