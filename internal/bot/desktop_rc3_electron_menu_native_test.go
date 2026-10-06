package bot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

// This opt-in stage opens the full POC-style Electron context menu exactly once.
// It saves the original receipt and fresh, bounded AX candidates before a
// separate stage decides which observed target can be invoked.
func TestPackagedRC3ElectronContextMenuObserve(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_LOG")
	markerPath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_OPEN_ONCE")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_OBSERVATION")
	run := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_RUN")
	if helper == "" || title == "" || logPath == "" || markerPath == "" || evidencePath == "" || run == "" {
		t.Skip("requires packaged rc.3 helper, new isolated Electron menu fixture and durable paths")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	r, _, _ := fixture(t)
	r.ConfigureDesktopControl(desktopcontrol.New(helper, t.TempDir()))
	r.BeginDesktopTurn()
	defer r.StopDesktopTurn()
	evidence := map[string]any{"fixture": "isolated Electron full-POC-style context menu", "title": title}
	defer func() {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err == nil {
			_ = os.WriteFile(evidencePath, b, 0600)
		}
	}()
	call := func(name string, request any) api.ToolResult {
		b, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return r.CallTool(ctx, name, b)
	}
	decode := func(out api.ToolResult, target any) {
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, target); err != nil {
			t.Fatal(err)
		}
	}
	inspect := func(scope, match any, maxDepth int) rc3ProjectedObservation {
		t.Helper()
		request := map[string]any{"type": "outline", "scope": scope, "projection": "outline",
			"fields": []string{"name", "role", "app", "states", "capabilities"},
			"budget": map[string]any{"max_depth": maxDepth, "max_visited_nodes": 10000,
				"max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 10000}}
		if match != nil {
			request["match"] = match
		}
		out := call("bot_desktop_inspect", map[string]any{"request": request})
		if out.IsError {
			t.Fatal("compact Electron observation refused", out.StructuredContent["error"])
		}
		var observed rc3ProjectedObservation
		decode(out, &observed)
		return observed
	}
	inventoryReply := call("bot_desktop_inspect", map[string]any{"request": map[string]any{
		"type": "outline", "scope": map[string]any{"desktop": true}, "projection": "summary",
		"fields": []string{"name", "role", "app"},
		"budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000},
	}})
	if inventoryReply.IsError {
		t.Fatal("compact Electron inventory refused", inventoryReply.StructuredContent["error"])
	}
	var inventory rc3ProjectedObservation
	decode(inventoryReply, &inventory)
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Known == title {
			if window != "" {
				t.Fatal("owned Electron window title is ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated Electron menu window absent")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app {
			appName = o.Name.Known
		}
	}
	if appName == "" {
		t.Fatal("Electron application identity absent")
	}
	if out := call("bot_desktop_authorize", map[string]any{"application": app, "name": appName,
		"purpose": "inspect and invoke one disposable Electron context menu command"}); out.IsError {
		t.Fatal("Electron native grant refused", out.StructuredContent["error"])
	}
	canvas := inspect(map[string]any{"refs": []dw.Ref{window}},
		map[string]any{"within": window, "name_equals": "POC Canvas"}, 32)
	if !canvas.Coverage.Complete || len(canvas.Objects) != 1 {
		t.Fatalf("synthetic canvas not unique: count=%d complete=%t", len(canvas.Objects), canvas.Coverage.Complete)
	}
	before, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(before), `"event":"ready"`) ||
		strings.Contains(string(before), `"event":"right"`) {
		t.Fatal("Electron fixture not ready or menu already attempted")
	}
	if err := os.MkdirAll(filepath.Dir(markerPath), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("menu opening already attempted; no replay", err)
	}
	_, writeErr := f.WriteString(title + "\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		t.Fatal("durable menu marker failed before input", err)
	}
	combined := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_COMBINED") == "1"
	requestID := "rc3-electron-menu-open-" + run + "-0001"
	steps := []any{
		map[string]any{"id": "open", "op": "pointer.click", "target": map[string]any{
			"anchor": map[string]any{"target": canvas.Objects[0].Ref, "u": 0.5, "v": 0.5}},
			"click": map[string]any{"button": "right", "count": 1}, "completion": "dispatch"},
	}
	if combined {
		requestID = "rc3-electron-menu-combined-" + run + "-0001"
		steps = append(steps,
			map[string]any{"id": "bind", "op": "bind", "bind": map[string]any{"name": "menu",
				"locator": map[string]any{"within": window, "name_equals": "POC menu commit",
					"role": "menu_item", "max_depth": 12}, "require_unique": true}},
			map[string]any{"id": "invoke", "op": "invoke", "target": map[string]any{"bound": "menu"},
				"completion": "dispatch"})
	}
	opened := call("bot_desktop_act", map[string]any{"requestId": requestID, "steps": steps})
	var receipt dw.Receipt
	receiptBytes, err := json.Marshal(opened.StructuredContent["result"])
	if err != nil || protocol.Decode(receiptBytes, &receipt) != nil {
		t.Fatal("original menu-open receipt unavailable; no replay", err)
	}
	status := call("bot_desktop_result", map[string]any{"request": map[string]any{"type": "status", "requestId": requestID}})
	var reconciled dw.Receipt
	statusBytes, err := json.Marshal(status.StructuredContent["result"])
	if err != nil || protocol.Decode(statusBytes, &reconciled) != nil {
		t.Fatal("same-ID menu-open receipt unavailable; no replay", err)
	}
	evidence["open"] = map[string]any{"request_id": requestID, "receipt": receipt, "combined": combined,
		"same_run_reconcile": receipt.RunID != "" && receipt.RunID == reconciled.RunID}
	if receipt.RunID == "" || reconciled.RunID != receipt.RunID || len(receipt.Steps) != len(steps) {
		t.Fatal("menu open uncertain; original receipt retained, no replay")
	}
	if !combined && (opened.IsError || receipt.Outcome != "completed") {
		t.Fatal("menu open uncertain; original receipt retained, no replay")
	}
	if combined && !(receipt.Outcome == "completed" && !opened.IsError ||
		receipt.Outcome == "stopped" && opened.IsError && receipt.Fault != nil &&
			receipt.Fault.Code == "ambiguous_target") {
		t.Fatal("combined menu plan unexpected; original receipt retained, no replay")
	}
	var log []byte
	for i := 0; i < 40; i++ {
		log, err = os.ReadFile(logPath)
		if err == nil && strings.Count(string(log), `"event":"right"`) == 1 &&
			strings.Count(string(log), `"event":"menu_visible","value":"true"`) == 1 &&
			(!combined || receipt.Outcome != "completed" ||
				strings.Count(string(log), `"event":"menu_commit"`) == 1) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	commitCount := strings.Count(string(log), `"event":"menu_commit"`)
	if strings.Count(string(log), `"event":"right"`) != 1 ||
		(!combined && commitCount != 0) ||
		(combined && receipt.Outcome == "stopped" && commitCount != 0) ||
		(combined && receipt.Outcome == "completed" && commitCount != 1) {
		t.Fatal("right-click business effect not independently confirmed; no second input")
	}
	evidence["after_open_log"] = map[string]any{"right_count": 1, "menu_commit_count": commitCount}
	queries := []struct {
		name  string
		root  dw.Ref
		match map[string]any
		depth int
	}{
		{"original", window, map[string]any{"within": window, "name_equals": "POC menu commit", "role": "menu_item"}, 12},
		{"window_name", window, map[string]any{"within": window, "name_equals": "POC menu commit"}, 32},
		{"app_name", app, map[string]any{"within": app, "name_equals": "POC menu commit"}, 32},
	}
	results := map[string]any{}
	for _, query := range queries {
		observed := inspect(map[string]any{"refs": []dw.Ref{query.root}}, query.match, query.depth)
		objects := make([]map[string]any, 0, len(observed.Objects))
		for _, o := range observed.Objects {
			objects = append(objects, map[string]any{"kind": o.Kind, "role": o.Role,
				"name": o.Name.Known, "capabilities": o.Capabilities})
		}
		results[query.name] = map[string]any{"count": len(objects),
			"complete": observed.Coverage.Complete, "objects": objects}
	}
	evidence["fresh_observation"] = results
	t.Logf("rc.3 Electron menu once-plan outcome=%s; fresh bounded candidates and original receipt saved", receipt.Outcome)
}

// Runs only after TestPackagedRC3ElectronContextMenuObserve left the owned
// synthetic menu open. It binds the freshly reobserved unique menu item and
// checks one independent app-owned business effect.
func TestPackagedRC3ElectronContextMenuInvoke(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_LOG")
	markerPath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_INVOKE_ONCE")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_INVOKE_EVIDENCE")
	if helper == "" || title == "" || logPath == "" || markerPath == "" || evidencePath == "" {
		t.Skip("requires an opened isolated Electron menu and new invoke marker")
	}
	before, err := os.ReadFile(logPath)
	rightBefore := strings.Count(string(before), `"event":"right"`)
	if err != nil || rightBefore < 1 ||
		strings.Count(string(before), `"event":"menu_visible","value":"true"`) != 1 ||
		strings.Count(string(before), `"event":"menu_commit"`) != 0 {
		t.Fatal("isolated menu is not at the once-opened, zero-commit baseline")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	r, _, _ := fixture(t)
	r.ConfigureDesktopControl(desktopcontrol.New(helper, t.TempDir()))
	r.BeginDesktopTurn()
	defer r.StopDesktopTurn()
	evidence := map[string]any{"fixture": "isolated Electron full-POC-style context menu"}
	defer func() {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err == nil {
			_ = os.WriteFile(evidencePath, b, 0600)
		}
	}()
	call := func(name string, request any) api.ToolResult {
		b, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return r.CallTool(ctx, name, b)
	}
	project := func(out api.ToolResult) rc3ProjectedObservation {
		t.Helper()
		if out.IsError {
			t.Fatal("compact Electron observation refused", out.StructuredContent["error"])
		}
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			t.Fatal(err)
		}
		var observed rc3ProjectedObservation
		if err := json.Unmarshal(b, &observed); err != nil {
			t.Fatal(err)
		}
		return observed
	}
	inventory := project(call("bot_desktop_inspect", map[string]any{"request": map[string]any{
		"type": "outline", "scope": map[string]any{"desktop": true}, "projection": "summary",
		"fields": []string{"name", "role", "app"},
		"budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000},
	}}))
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Known == title {
			if window != "" {
				t.Fatal("owned Electron window ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("isolated Electron menu window absent")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app {
			appName = o.Name.Known
		}
	}
	if appName == "" {
		t.Fatal("Electron application identity absent")
	}
	if out := call("bot_desktop_authorize", map[string]any{"application": app, "name": appName,
		"purpose": "invoke one observed synthetic context menu command"}); out.IsError {
		t.Fatal("Electron native grant refused", out.StructuredContent["error"])
	}
	selector := map[string]any{"within": window, "name_equals": "POC menu commit", "role": "menu_item"}
	observed := project(call("bot_desktop_inspect", map[string]any{"request": map[string]any{
		"type": "outline", "scope": map[string]any{"refs": []dw.Ref{window}},
		"projection": "outline", "fields": []string{"name", "role", "capabilities"},
		"match": selector,
		"budget": map[string]any{"max_depth": 12, "max_visited_nodes": 10000,
			"max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000},
	}}))
	if !observed.Coverage.Complete || len(observed.Objects) != 1 {
		t.Fatalf("fresh menu item not unique: count=%d complete=%t", len(observed.Objects), observed.Coverage.Complete)
	}
	item := observed.Objects[0]
	available := false
	for _, cap := range item.Capabilities {
		available = available || cap.Name == "invoke" && cap.Support == "supported" && cap.Availability == "available"
	}
	if item.Role != "menu_item" || item.Name.Known != "POC menu commit" || !available {
		t.Fatal("fresh menu item does not advertise a safe semantic invoke")
	}
	evidence["fresh_candidate"] = map[string]any{"count": 1, "complete": true,
		"role": item.Role, "name": item.Name.Known, "invoke": "supported/available"}
	broader := project(call("bot_desktop_inspect", map[string]any{"request": map[string]any{
		"type": "outline", "scope": map[string]any{"refs": []dw.Ref{window}},
		"projection": "outline", "fields": []string{"name", "role", "capabilities"},
		"match": map[string]any{"within": window, "name_equals": "POC menu commit"},
		"budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000,
			"max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000},
	}}))
	evidence["fresh_window_name"] = map[string]any{"count": len(broader.Objects),
		"complete": broader.Coverage.Complete}
	if os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_INSPECT_ONLY") == "1" {
		t.Log("rc.3 Bot compact fresh Electron menu candidates saved without new input")
		return
	}
	if os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_RUN") == "" {
		t.Fatal("new run suffix required for a distinct menu invoke request ID")
	}
	f, err := os.OpenFile(markerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("menu invoke already attempted; no replay", err)
	}
	_, writeErr := f.WriteString(title + "\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		t.Fatal("durable invoke marker failed before input", err)
	}
	requestID := "rc3-electron-menu-invoke-" +
		os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_MENU_RUN") + "-0001"
	out := call("bot_desktop_act", map[string]any{"requestId": requestID, "steps": []any{
		map[string]any{"id": "bind", "op": "bind", "bind": map[string]any{"name": "menu",
			"locator": map[string]any{"within": window, "name_equals": "POC menu commit",
				"role": "menu_item", "max_depth": 12}, "require_unique": true}},
		map[string]any{"id": "invoke", "op": "invoke", "target": map[string]any{"bound": "menu"},
			"completion": "dispatch"},
	}})
	decodeReceipt := func(out api.ToolResult) dw.Receipt {
		t.Helper()
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			t.Fatal("menu receipt unavailable; no replay", err)
		}
		var receipt dw.Receipt
		if err := protocol.Decode(b, &receipt); err != nil {
			t.Fatal("menu receipt undecodable; no replay", err)
		}
		return receipt
	}
	receipt := decodeReceipt(out)
	status := call("bot_desktop_result", map[string]any{"request": map[string]any{"type": "status", "requestId": requestID}})
	reconciled := decodeReceipt(status)
	evidence["invoke"] = map[string]any{"request_id": requestID, "receipt": receipt,
		"same_run_reconcile": receipt.RunID != "" && receipt.RunID == reconciled.RunID}
	if out.IsError || receipt.Outcome != "completed" || len(receipt.Steps) != 2 ||
		receipt.RunID == "" || receipt.RunID != reconciled.RunID {
		t.Fatal("menu invoke uncertain; original receipt retained, no replay")
	}
	var log []byte
	for i := 0; i < 40; i++ {
		log, err = os.ReadFile(logPath)
		if err == nil && strings.Count(string(log), `"event":"menu_commit"`) == 1 &&
			strings.Count(string(log), `"event":"menu_visible","value":"false"`) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	evidence["app_log"] = map[string]any{"right_count": strings.Count(string(log), `"event":"right"`),
		"menu_commit_count": strings.Count(string(log), `"event":"menu_commit"`),
		"menu_hidden_count": strings.Count(string(log), `"event":"menu_visible","value":"false"`)}
	if strings.Count(string(log), `"event":"right"`) != rightBefore ||
		strings.Count(string(log), `"event":"menu_commit"`) != 1 ||
		strings.Count(string(log), `"event":"menu_visible","value":"false"`) != 1 {
		t.Fatal("one synthetic menu business effect not independently confirmed; no retry")
	}
	t.Log("rc.3 Bot compact bind/invoke on fresh unique Electron menu item produced exactly one app-owned effect")
}
