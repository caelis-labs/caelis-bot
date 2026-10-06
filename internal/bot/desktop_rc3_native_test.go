package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

type rc3ProjectedObservation struct {
	Objects []struct {
		Ref  dw.Ref  `json:"ref"`
		App  dw.Ref  `json:"app"`
		Kind dw.Kind `json:"kind"`
		Role string  `json:"role"`
		Name struct {
			Known string `json:"known"`
		} `json:"name"`
		States map[string]struct {
			Known *bool `json:"known"`
		} `json:"states"`
		Capabilities []struct {
			Name         string `json:"name"`
			Support      string `json:"support"`
			Availability string `json:"availability"`
		} `json:"capabilities"`
	} `json:"objects"`
	Coverage struct {
		Complete bool `json:"complete"`
	} `json:"coverage"`
}

type rc3DOMState struct {
	Title           string `json:"title"`
	Checked         bool   `json:"checked"`
	DisabledChecked bool   `json:"disabledChecked"`
	Events          []struct {
		Type    string `json:"type"`
		Checked bool   `json:"checked"`
	} `json:"events"`
}

// This opt-in test uses the real four compact Bot tools and packaged public
// helper. Every intended effect has its own durable marker and request ID;
// a failed/unknown step stops the sequence without attempting another input.
func TestPackagedRC3ChromeCompactCheckbox(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	profile := os.Getenv("CAELIS_BOT_TEST_RC3_CHROME_PROFILE")
	run := os.Getenv("CAELIS_BOT_TEST_RC3_CHROME_RUN")
	markerDir := os.Getenv("CAELIS_BOT_TEST_RC3_CHROME_MARKERS")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_RC3_CHROME_EVIDENCE")
	if helper == "" || profile == "" || run == "" || markerDir == "" || evidencePath == "" {
		t.Skip("requires published packaged helper, fresh private Chrome profile/page and evidence paths")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	r, _, _ := fixture(t)
	r.ConfigureDesktopControl(desktopcontrol.New(helper, t.TempDir()))
	r.BeginDesktopTurn()
	defer r.StopDesktopTurn()
	evidence := map[string]any{"release": "v0.1.0-rc.3", "fixture": "private Chrome profile and synthetic file page"}
	defer func() {
		b, err := json.MarshalIndent(evidence, "", "  ")
		if err == nil {
			_ = os.WriteFile(evidencePath, b, 0600)
		}
	}()
	call := func(name string, request any) api.ToolResult {
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return r.CallTool(ctx, name, body)
	}
	decode := func(out api.ToolResult, target any) error {
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			return err
		}
		return json.Unmarshal(b, target)
	}
	inspect := func(request any) rc3ProjectedObservation {
		t.Helper()
		out := call("bot_desktop_inspect", map[string]any{"request": request})
		if out.IsError {
			t.Fatalf("compact inspect refused: %v", out.StructuredContent["error"])
		}
		var observed rc3ProjectedObservation
		if err := decode(out, &observed); err != nil {
			t.Fatal("compact observation projection: ", err)
		}
		return observed
	}
	title := "Bot RC3 Chrome Checkbox Fixture " + run
	inventory := inspect(map[string]any{"type": "outline", "scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && strings.HasPrefix(o.Name.Known, title+" - Google Chrome") {
			if window != "" {
				t.Fatal("synthetic Chrome title ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("private Chrome fixture window absent")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app {
			appName = o.Name.Known
		}
	}
	if appName != "Chrome" && appName != "Google Chrome" {
		t.Fatalf("isolated browser identity mismatch: %q", appName)
	}
	if out := call("bot_desktop_authorize", map[string]any{"application": app, "name": appName, "purpose": "one disposable rc.3 checkbox acceptance"}); out.IsError {
		t.Fatal("compact native app grant refused", out.StructuredContent["error"])
	}
	if out := call("bot_desktop_inspect", map[string]any{"request": map[string]any{"type": "grants"}}); out.IsError || !strings.Contains(out.Content[0]["text"], "active") {
		t.Fatal("compact grant not active")
	}
	outline := inspect(map[string]any{"type": "outline", "scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_depth": 14, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	var document dw.Ref
	for _, o := range outline.Objects {
		if o.Role == "document" && o.Name.Known == title {
			document = o.Ref
		}
	}
	if document == "" {
		t.Fatal("unique synthetic document not observed")
	}
	findCheck := func(name string) (dw.Ref, string) {
		t.Helper()
		result := inspect(map[string]any{"type": "outline", "scope": map[string]any{"refs": []dw.Ref{document}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": document, "role": "checkbox", "name_equals": name}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}})
		if !result.Coverage.Complete || len(result.Objects) != 1 || result.Objects[0].Role != "checkbox" {
			t.Fatal("checkbox identity or coverage incomplete", name)
		}
		o := result.Objects[0]
		var capability string
		for _, cap := range o.Capabilities {
			if cap.Name == "set_checked" {
				capability = cap.Support + "/" + cap.Availability
			}
		}
		return o.Ref, capability
	}
	checkbox, support := findCheck("Bot RC3 checkbox")
	if support != "supported/available" {
		t.Fatal("Chrome checkbox semantic action unavailable", support)
	}
	readDOM := func() rc3DOMState {
		t.Helper()
		root, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "script/desktop-world-rc3-browser-readback.mjs"), profile, run)
		cmd.Dir = root
		output, err := cmd.Output()
		if err != nil {
			t.Fatal("independent private-page DOM readback failed: ", err)
		}
		var state rc3DOMState
		if err := json.Unmarshal(output, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if before := readDOM(); before.Checked || before.DisabledChecked || len(before.Events) != 0 || before.Title != title {
		t.Fatal("new private page has a nonempty baseline")
	}
	mark := func(stage string) {
		t.Helper()
		f, err := os.OpenFile(filepath.Join(markerDir, stage+".once"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal("stage already attempted; no replay", stage, err)
		}
		_, writeErr := f.WriteString(title + "\n")
		syncErr := f.Sync()
		closeErr := f.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			t.Fatal("durable marker failed before input", err)
		}
	}
	readReceipt := func(out api.ToolResult) dw.Receipt {
		t.Helper()
		b, err := json.Marshal(out.StructuredContent["result"])
		if err != nil {
			t.Fatal(err)
		}
		var receipt dw.Receipt
		if err := protocol.Decode(b, &receipt); err != nil {
			t.Fatal("original receipt unavailable; no replay", err)
		}
		return receipt
	}
	act := func(stage, id string, target dw.Ref, checked bool, expectedOutcome, expectedDelivery string) {
		t.Helper()
		mark(stage)
		out := call("bot_desktop_act", map[string]any{"requestId": id, "steps": []any{map[string]any{"id": "set", "op": "set_checked", "target": map[string]any{"ref": target}, "set_checked": map[string]any{"checked": checked}, "completion": "verify"}}})
		receipt := readReceipt(out)
		status := call("bot_desktop_result", map[string]any{"request": map[string]any{"type": "status", "requestId": id}})
		recovered := readReceipt(status)
		evidence[stage] = map[string]any{"request_id": id, "outcome": receipt.Outcome, "delivery": receipt.Steps, "same_run_reconcile": receipt.RunID != "" && recovered.RunID == receipt.RunID, "result_error": out.IsError}
		if receipt.RunID == "" || recovered.RunID != receipt.RunID || receipt.Outcome != expectedOutcome || len(receipt.Steps) != 1 || string(receipt.Steps[0].Delivery) != expectedDelivery || out.IsError != (expectedOutcome != "completed") {
			t.Fatalf("%s original receipt unexpected; no replay: outcome=%s error=%t", stage, receipt.Outcome, out.IsError)
		}
	}
	assertDOM := func(stage string, checked bool, events []struct {
		Type    string
		Checked bool
	}) {
		t.Helper()
		state := readDOM()
		got := make([]struct {
			Type    string
			Checked bool
		}, 0, len(state.Events))
		for _, event := range state.Events {
			got = append(got, struct {
				Type    string
				Checked bool
			}{event.Type, event.Checked})
		}
		if state.Checked != checked || state.DisabledChecked || !reflect.DeepEqual(got, events) {
			t.Fatalf("%s independent DOM mismatch: checked=%t events=%v", stage, state.Checked, got)
		}
		evidence[stage+"_dom"] = map[string]any{"checked": checked, "events": got}
	}
	onEvents := []struct {
		Type    string
		Checked bool
	}{{"input", true}, {"change", true}}
	allEvents := append(append([]struct {
		Type    string
		Checked bool
	}{}, onEvents...), struct {
		Type    string
		Checked bool
	}{"input", false}, struct {
		Type    string
		Checked bool
	}{"change", false})
	act("on", "rc3-chrome-on-0001", checkbox, true, "completed", "complete")
	assertDOM("on", true, onEvents)
	act("idempotent", "rc3-chrome-idempotent-0001", checkbox, true, "completed", "not_applicable")
	assertDOM("idempotent", true, onEvents)
	act("off", "rc3-chrome-off-0001", checkbox, false, "completed", "complete")
	assertDOM("off", false, allEvents)
	disabled, _ := findCheck("Bot RC3 disabled checkbox")
	act("disabled", "rc3-chrome-disabled-0001", disabled, true, "stopped", "none")
	assertDOM("disabled", false, allEvents)
	evidence["final"] = map[string]any{"checked": false, "input_events": 2, "change_events": 2, "pointer_fallback": false}
	fmt.Fprintln(os.Stdout, "RC3 compact Chrome checkbox and independent DOM PASS")
}

// A missing context command must not be invented by Bot's semantic binding.
// This checks the fail-closed path, not the upstream full Electron script.
func TestPackagedRC3ElectronAmbiguousBind(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	logPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_ELECTRON_LOG")
	oncePath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_BIND_ONCE")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_RC3_ELECTRON_BIND_EVIDENCE")
	if helper == "" || logPath == "" || oncePath == "" || evidencePath == "" {
		t.Skip("requires one owned Electron fixture and a fresh once marker")
	}
	before, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(before), `"event":"action_count","value":"1"`) {
		t.Fatal("synthetic Electron action baseline missing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	r, _, _ := fixture(t)
	r.ConfigureDesktopControl(desktopcontrol.New(helper, t.TempDir()))
	r.BeginDesktopTurn()
	defer r.StopDesktopTurn()
	call := func(name string, request any) api.ToolResult {
		b, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		return r.CallTool(ctx, name, b)
	}
	observe := call("bot_desktop_inspect", map[string]any{"request": map[string]any{"type": "outline", "scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}})
	if observe.IsError {
		t.Fatal("Electron inventory refused before input")
	}
	b, _ := json.Marshal(observe.StructuredContent["result"])
	var inventory rc3ProjectedObservation
	if err := json.Unmarshal(b, &inventory); err != nil {
		t.Fatal(err)
	}
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Known == "RC2 Electron Synthetic Document" {
			if window != "" {
				t.Fatal("Electron fixture ambiguous before input")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("owned Electron window absent")
	}
	var appName string
	for _, o := range inventory.Objects {
		if o.Ref == app {
			appName = o.Name.Known
		}
	}
	if appName == "" {
		t.Fatal("Electron app identity absent")
	}
	if out := call("bot_desktop_authorize", map[string]any{"application": app, "name": appName, "purpose": "verify unique semantic binding refusal in owned Electron fixture"}); out.IsError {
		t.Fatal("Electron grant refused")
	}
	f, err := os.OpenFile(oncePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("Electron bind plan already attempted", err)
	}
	_, writeErr := f.WriteString("owned synthetic Electron context binding\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		t.Fatal(err)
	}
	const requestID = "rc3-electron-ambiguous-bind-0001"
	out := call("bot_desktop_act", map[string]any{"requestId": requestID, "steps": []any{
		map[string]any{"id": "bind", "op": "bind", "bind": map[string]any{"name": "menu", "locator": map[string]any{"within": window, "kind": "ui", "role": "menu_item", "name_equals": "Synthetic absent context command", "max_depth": 32}, "require_unique": true}},
		map[string]any{"id": "would-invoke", "op": "invoke", "target": map[string]any{"bound": "menu"}, "completion": "dispatch"},
	}})
	b, _ = json.Marshal(out.StructuredContent["result"])
	var receipt dw.Receipt
	if err := protocol.Decode(b, &receipt); err != nil {
		t.Fatal("original Electron bind receipt missing; no replay", err)
	}
	status := call("bot_desktop_result", map[string]any{"request": map[string]any{"type": "status", "requestId": requestID}})
	b, _ = json.Marshal(status.StructuredContent["result"])
	var recovered dw.Receipt
	if err := protocol.Decode(b, &recovered); err != nil {
		t.Fatal("same-ID Electron bind receipt missing", err)
	}
	after, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	proof := map[string]any{"outcome": receipt.Outcome, "same_run_reconcile": receipt.RunID != "" && receipt.RunID == recovered.RunID,
		"fault": receipt.Fault, "steps": receipt.Steps, "app_log_unchanged": string(before) == string(after)}
	b, _ = json.MarshalIndent(proof, "", "  ")
	if err := os.WriteFile(evidencePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if !out.IsError || receipt.Outcome != "stopped" || receipt.Fault == nil || receipt.Fault.Code != "ambiguous_target" || len(receipt.Steps) != 2 || receipt.Steps[0].Delivery != dw.DeliveryNA || receipt.Steps[1].State != "skipped" || receipt.RunID == "" || receipt.RunID != recovered.RunID || string(before) != string(after) {
		t.Fatal("Electron context bind did not refuse before delivery; no replay")
	}
	t.Log("rc.3 Bot bind refused zero-match Electron context command before delivery; same receipt, no app effect")
}
