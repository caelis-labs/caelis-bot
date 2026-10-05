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

func TestPackagedHelperNativeRC2Electron(t *testing.T) {
	helper, logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_ELECTRON_LOG")
	if helper == "" || logPath == "" {
		t.Skip("requires isolated standalone Electron page and packaged helper")
	}
	before, err := fixtureEvents(logPath)
	if err != nil || !before["ready:RC2 Electron Verified 20261006"] {
		t.Fatal("standalone Electron fixture not ready")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "rc2-electron"), 45*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, Prefix+op, raw)
		if out.IsError {
			if op == "act" && c.requests[id] != nil {
				t.Logf("original electron receipt: %s", c.requests[id].reply.Result)
				_ = c.reconcile(ctx, id)
			}
			t.Fatalf("%s failed: %v", id, out.StructuredContent["error"])
		}
		return c.requests[id].reply.Result
	}
	var inventory dw.Observation
	if err := protocol.Decode(call("electron-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}), &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && strings.Contains(*o.Name.Value, "RC2 Electron Synthetic Document") {
			if window != "" {
				t.Fatal("Electron fixture window ambiguous")
			}
			window, app = o.Ref, o.App
			t.Logf("Electron window title=%s", *o.Name.Value)
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated Electron window unavailable")
	}
	var name string
	for _, o := range inventory.Objects {
		if o.Ref == app && o.Name.Value != nil {
			name = *o.Name.Value
		}
	}
	if name == "" {
		t.Fatal("Electron app name unavailable")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(app), "name": name, "purpose": "isolated standalone Electron acceptance"})
	if out := c.CallTool(ctx, Prefix+"authorize", auth); out.IsError {
		t.Fatal("Electron grant refused", out.StructuredContent)
	}
	var target dw.Observation
	if err := protocol.Decode(call("electron-button", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "capabilities", "states"}, "match": map[string]any{"within": window, "name_equals": "Record Electron action"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}}), &target); err != nil {
		t.Fatal(err)
	}
	if !target.Coverage.Complete || len(target.Objects) != 1 {
		t.Fatalf("Electron button not unique: count=%d complete=%t", len(target.Objects), target.Coverage.Complete)
	}
	button := target.Objects[0]
	var invoke string
	for _, cap := range button.Capabilities {
		if cap.Name == "invoke" {
			invoke = cap.Support + "/" + cap.Availability
		}
	}
	t.Logf("Electron button role=%s invoke=%s", button.Role, invoke)
	if os.Getenv("CAELIS_BOT_TEST_DESKTOP_ELECTRON_ACT") != "1" {
		return
	}
	if invoke != "supported/available" || before["action_count:1"] {
		t.Fatal("Electron action baseline or capability unavailable; no input sent")
	}
	once := os.Getenv("CAELIS_BOT_TEST_DESKTOP_ELECTRON_ONCE")
	if once == "" {
		t.Fatal("Electron once marker required")
	}
	f, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("Electron input already attempted", err)
	}
	_, writeErr := f.WriteString("RC2 Electron Verified 20261006\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("Electron marker failed", writeErr, closeErr)
	}
	var receipt dw.Receipt
	if err := protocol.Decode(call("electron-invoke-once", "act", map[string]any{"steps": []map[string]any{{"id": "invoke-electron", "op": "invoke", "target": map[string]any{"ref": button.Ref}, "completion": "dispatch"}}}), &receipt); err != nil || receipt.Outcome != "completed" {
		t.Fatalf("Electron action uncertain: outcome=%s err=%v", receipt.Outcome, err)
	}
	for i := 0; i < 20; i++ {
		after, err := fixtureEvents(logPath)
		if err == nil && after["action_count:1"] {
			break
		}
		if i == 19 {
			t.Fatal("app-owned Electron action event absent")
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.EndTurn("rc2-electron")
	if out := c.reconcile(context.Background(), "electron-invoke-once"); out.IsError {
		t.Fatal("Electron original receipt lost", out.StructuredContent)
	}
	t.Log("published helper Electron invoke and independent app-owned action count passed")
}
