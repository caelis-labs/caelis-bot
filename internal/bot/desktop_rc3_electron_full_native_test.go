package bot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

type rc3ElectronEvent struct {
	Event string `json:"event"`
	Value string `json:"value"`
}

func rc3ElectronEvents(path string) ([]rc3ElectronEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []rc3ElectronEvent
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var event rc3ElectronEvent
		if err := json.Unmarshal(scan.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scan.Err()
}

// Opt-in: one new private Electron profile reproduces the eleven full-POC
// pre-menu operations before the original immediate right-click/bind/invoke
// plan. Every stage has a durable marker and same-ID receipt reconciliation.
func TestPackagedRC3ElectronFullPreMenu(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_FULL_TITLE")
	logPath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_FULL_LOG")
	markerDir := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_FULL_MARKERS")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_FULL_EVIDENCE")
	run := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_FULL_RUN")
	if helper == "" || title == "" || logPath == "" || markerDir == "" || evidencePath == "" || run == "" {
		t.Skip("requires new full-POC-style Electron instance, packaged helper and durable paths")
	}
	initial, err := rc3ElectronEvents(logPath)
	if err != nil || len(initial) == 0 || initial[0].Event != "ready" || initial[0].Value != title {
		t.Fatal("new isolated full Electron fixture is not at its ready baseline")
	}
	for _, event := range initial[1:] {
		if event.Event != "move" {
			t.Fatal("new full Electron fixture has a preexisting business effect")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	r, _, _ := fixture(t)
	r.ConfigureDesktopControl(desktopcontrol.New(helper, t.TempDir()))
	r.BeginDesktopTurn()
	defer r.StopDesktopTurn()
	evidence := map[string]any{"fixture": "new private Electron full-POC-style page", "title": title}
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
		var observation rc3ProjectedObservation
		if err := json.Unmarshal(b, &observation); err != nil {
			t.Fatal(err)
		}
		return observation
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
				t.Fatal("private full Electron title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("private full Electron window absent")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app {
			appName = o.Name.Known
		}
	}
	if appName == "" {
		t.Fatal("private full Electron application name absent")
	}
	if out := call("bot_desktop_authorize", map[string]any{"application": app, "name": appName,
		"purpose": "one full synthetic Electron desktop acceptance sequence"}); out.IsError {
		t.Fatal("private Electron native grant refused", out.StructuredContent["error"])
	}
	find := func(name string) dw.Ref {
		t.Helper()
		observed := project(call("bot_desktop_inspect", map[string]any{"request": map[string]any{
			"type": "outline", "scope": map[string]any{"refs": []dw.Ref{window}},
			"projection": "outline", "fields": []string{"name", "role", "capabilities"},
			"match": map[string]any{"within": window, "name_equals": name},
			"budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000,
				"max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000},
		}}))
		if !observed.Coverage.Complete || len(observed.Objects) != 1 {
			t.Fatalf("private Electron target %q not unique: count=%d complete=%t", name, len(observed.Objects), observed.Coverage.Complete)
		}
		return observed.Objects[0].Ref
	}
	field, multi, canvas, submit := find("POC text"), find("POC multiline"), find("POC Canvas"), find("POC submit")
	_ = find("POC dialog")
	ref := func(r dw.Ref) map[string]any { return map[string]any{"ref": r} }
	anchor := func(r dw.Ref, u, v float64) map[string]any {
		return map[string]any{"anchor": map[string]any{"target": r, "u": u, "v": v}}
	}
	click := func(r dw.Ref, u, v float64, button string, count int) map[string]any {
		return map[string]any{"op": "pointer.click", "target": anchor(r, u, v),
			"click": map[string]any{"button": button, "count": count}}
	}
	decodeReceipt := func(out api.ToolResult) dw.Receipt {
		t.Helper()
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			t.Fatal("original receipt unavailable; no replay", err)
		}
		var receipt dw.Receipt
		if err := protocol.Decode(b, &receipt); err != nil {
			t.Fatal("original receipt undecodable; no replay", err)
		}
		return receipt
	}
	if err := os.MkdirAll(markerDir, 0700); err != nil {
		t.Fatal(err)
	}
	act := func(stage string, steps []map[string]any, event, value string) dw.Receipt {
		t.Helper()
		before, err := rc3ElectronEvents(logPath)
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(filepath.Join(markerDir, stage+".once"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(stage, "already attempted; no replay", err)
		}
		_, writeErr := f.WriteString(title + "\n")
		syncErr := f.Sync()
		closeErr := f.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			t.Fatal(stage, "marker failed before input", err)
		}
		for i := range steps {
			steps[i]["id"] = stage + "-" + string(rune('a'+i))
		}
		id := "rc3-electron-full-" + run + "-" + stage
		out := call("bot_desktop_act", map[string]any{"requestId": id, "steps": steps})
		receipt := decodeReceipt(out)
		recovered := decodeReceipt(call("bot_desktop_result", map[string]any{"request": map[string]any{
			"type": "status", "requestId": id}}))
		row := map[string]any{"request_id": id, "receipt": receipt,
			"same_run_reconcile": receipt.RunID != "" && recovered.RunID == receipt.RunID}
		evidence[stage] = row
		if stage == "context-menu" && receipt.Outcome == "partial" && receipt.Fault != nil &&
			receipt.Fault.Code == "ambiguous_target" && receipt.RunID != "" &&
			recovered.RunID == receipt.RunID && len(receipt.Steps) == 3 &&
			receipt.Steps[0].Delivery == dw.DeliveryComplete &&
			receipt.Steps[1].Delivery == dw.DeliveryNA &&
			receipt.Steps[2].State == "skipped" {
			var after []rc3ElectronEvent
			for retry := 0; retry < 40; retry++ {
				after, err = rc3ElectronEvents(logPath)
				if err != nil {
					t.Fatal(err)
				}
				visible := false
				for _, item := range after[len(before):] {
					visible = visible || item.Event == "menu_visible" && item.Value == "true"
				}
				if visible {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			right, commits, visible := 0, 0, 0
			for _, item := range after[len(before):] {
				if item.Event == "right" {
					right++
				}
				if item.Event == "menu_commit" {
					commits++
				}
				if item.Event == "menu_visible" && item.Value == "true" {
					visible++
				}
			}
			row["app_events_after_partial"] = map[string]any{
				"right_count": right, "menu_visible_count": visible, "menu_commit_count": commits}
			if right < 1 || visible != 1 || commits != 0 {
				t.Fatal("partial menu plan has unexpected app effect; no replay")
			}
			fresh := project(call("bot_desktop_inspect", map[string]any{"request": map[string]any{
				"type": "outline", "scope": map[string]any{"refs": []dw.Ref{window}},
				"projection": "outline", "fields": []string{"name", "role", "capabilities"},
				"match": map[string]any{"within": window, "name_equals": "POC menu commit", "role": "menu_item"},
				"budget": map[string]any{"max_depth": 12, "max_visited_nodes": 10000,
					"max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000},
			}}))
			row["fresh_after_stopped_bind"] = map[string]any{"count": len(fresh.Objects),
				"complete": fresh.Coverage.Complete}
			if !fresh.Coverage.Complete || len(fresh.Objects) != 1 {
				t.Fatal("partial menu plan did not yield one fresh candidate; no replay")
			}
			return receipt
		}
		if out.IsError || receipt.Outcome != "completed" || receipt.RunID == "" ||
			recovered.RunID != receipt.RunID || len(receipt.Steps) != len(steps) {
			t.Fatal(stage, "receipt is not a completed original run; no replay")
		}
		found := false
		for retry := 0; retry < 40; retry++ {
			after, err := rc3ElectronEvents(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) < len(before) {
				t.Fatal("app event log was truncated; no retry")
			}
			for _, item := range after[len(before):] {
				if item.Event == event && (value == "" || item.Value == value) {
					if stage == "drag" || stage == "vertical-scroll" || stage == "horizontal-scroll" {
						parts := strings.Split(item.Value, ",")
						if len(parts) != 2 {
							continue
						}
						dx, dxErr := strconv.Atoi(parts[0])
						dy, dyErr := strconv.Atoi(parts[1])
						if dxErr != nil || dyErr != nil {
							continue
						}
						if stage == "drag" && !(dx > 100 && dy > -5 && dy < 5) ||
							stage == "vertical-scroll" && !(dx == 0 && dy != 0) ||
							stage == "horizontal-scroll" && !(dx != 0 && dy == 0) {
							continue
						}
					}
					found = true
				}
			}
			if found {
				row["app_event"] = map[string]any{"event": event, "value": value}
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !found {
			t.Fatal(stage, "app-owned effect missing; original receipt retained, no replay")
		}
		return receipt
	}
	act("move", []map[string]any{{"op": "pointer.move", "target": anchor(canvas, .3, .5)}}, "move", "")
	act("single", []map[string]any{click(canvas, .3, .5, "left", 1)}, "click", "1")
	act("double", []map[string]any{click(canvas, .3, .5, "left", 2)}, "double", "")
	act("middle", []map[string]any{click(canvas, .3, .5, "middle", 1)}, "middle", "1")
	act("drag", []map[string]any{{"op": "pointer.drag", "target": anchor(canvas, .2, .5),
		"drag": map[string]any{"to": anchor(canvas, .7, .5), "duration_ms": 250}}}, "drop", "")
	act("vertical-scroll", []map[string]any{{"op": "pointer.scroll", "target": anchor(canvas, .5, .5),
		"scroll": map[string]any{"dx": 0, "dy": 3, "unit": "wheel_step"}}}, "scroll", "")
	act("horizontal-scroll", []map[string]any{{"op": "pointer.scroll", "target": anchor(canvas, .5, .5),
		"scroll": map[string]any{"dx": 3, "dy": 0, "unit": "wheel_step"}}}, "scroll", "")
	text := "Full-中文-🙂"
	act("unicode-submit", []map[string]any{
		{"op": "pointer.click", "target": ref(field), "click": map[string]any{"button": "left", "count": 1}},
		{"op": "keyboard.type_text", "target": ref(field), "type_text": map[string]any{"text": text},
			"completion": "verify", "after": []any{map[string]any{"target": ref(field), "property": "value", "equals_string": text}}},
		{"op": "pointer.click", "target": ref(submit), "click": map[string]any{"button": "left", "count": 1}},
	}, "submit", text)
	act("shortcut-replace", []map[string]any{
		{"op": "pointer.click", "target": ref(field), "click": map[string]any{"button": "left", "count": 1}},
		{"op": "bind_focus", "bind_focus": map[string]any{"name": "input", "within": window}},
		{"op": "keyboard.press", "target": map[string]any{"bound": "input"}, "press": map[string]any{"key": "A", "modifiers": []string{"primary"}}},
		{"op": "keyboard.type_text", "target": map[string]any{"bound": "input"}, "type_text": map[string]any{"text": "replaced"}},
		{"op": "keyboard.press", "target": map[string]any{"bound": "input"}, "press": map[string]any{"key": "Left", "modifiers": []string{"shift"}}},
		{"op": "keyboard.press", "target": map[string]any{"bound": "input"}, "press": map[string]any{"key": "Backspace"}},
	}, "text", "replace")
	multiline := "Line1\n中文🙂\nLine3"
	act("multiline", []map[string]any{
		{"op": "pointer.click", "target": ref(multi), "click": map[string]any{"button": "left", "count": 1}},
		{"op": "keyboard.type_text", "target": ref(multi), "type_text": map[string]any{"text": multiline},
			"completion": "verify", "after": []any{map[string]any{"target": ref(multi), "property": "value", "equals_string": multiline}}},
	}, "multiline", multiline)
	act("window-key", []map[string]any{{"op": "keyboard.press", "target": ref(window),
		"press": map[string]any{"key": "Tab"}}}, "focus", "POC Canvas")
	menu := act("context-menu", []map[string]any{
		click(canvas, .5, .5, "right", 1),
		{"op": "bind", "bind": map[string]any{"name": "menu", "require_unique": true,
			"locator": map[string]any{"within": window, "name_equals": "POC menu commit",
				"role": "menu_item", "max_depth": 12}}},
		{"op": "invoke", "target": map[string]any{"bound": "menu"}},
	}, "menu_commit", "")
	evidence["menu_outcome"] = menu.Outcome
	if menu.Outcome == "partial" {
		t.Log("original immediate Electron bind reproduced ambiguous_target after 11 actions; fresh unique candidate, no menu effect or replay")
		return
	}
	final, err := rc3ElectronEvents(logPath)
	if err != nil {
		t.Fatal(err)
	}
	menuCommits := 0
	for _, event := range final {
		if event.Event == "menu_commit" {
			menuCommits++
		}
	}
	evidence["menu_commit_count"] = menuCommits
	if menuCommits != 1 {
		t.Fatal("full pre-menu sequence did not produce exactly one menu command")
	}
	t.Log("published rc.3 helper completed original full Electron pre-menu sequence and one context command through Bot compact tools")
}
