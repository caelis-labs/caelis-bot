package desktopcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func fixtureHost(t *testing.T, config map[string]any) (*Driver, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node required for the real host/pipe regression")
	}
	node, _ = filepath.Abs(node)
	root := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob("../../resources/computer-use/*.mjs")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, ".test.mjs") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Base(file), data)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules/@trycua/cua-driver"), 0700); err != nil {
		t.Fatal(err)
	}
	write("node_modules/@trycua/cua-driver/package.json", []byte(`{"type":"module","exports":"./index.mjs"}`))
	sdk, err := os.ReadFile("testdata/cua-sdk.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("node_modules/@trycua/cua-driver/index.mjs", sdk)
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	write("fixture.json", data)
	driver, err := StartDriver(node, filepath.Join(root, "host.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver, root
}

func hostCall(t *testing.T, driver *Driver, name string, input any) api.ToolResult {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	result := driver.CallTool(t.Context(), name, raw)
	if result.IsError {
		t.Fatalf("host call failed: %+v", result.Content)
	}
	return result
}

func TestHostCancellationDoesNotDispatchAfterPreflight(t *testing.T) {
	driver, root := fixtureHost(t, map[string]any{"blockPreflight": true})
	listed := hostCall(t, driver, "bot_desktop_observe", map[string]any{})
	window := listed.StructuredContent["windows"].([]any)[0].(map[string]any)["window"]
	observed := hostCall(t, driver, "bot_desktop_observe", map[string]any{"window": window})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	args, _ := json.Marshal(map[string]any{"observation": observed.StructuredContent["observation"], "steps": []any{map[string]any{"op": "type", "target": "e3", "text": "fixture"}}})
	done := make(chan api.ToolResult, 1)
	go func() { done <- driver.CallTool(ctx, "bot_desktop_perform", args) }()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "preflight-started")); err == nil {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("call ended before preflight: %+v", result.Content)
		case <-deadline:
			t.Fatal("helper did not reach preflight")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case result := <-done:
		if !result.IsError || !strings.Contains(result.Content[0]["text"], "unknown") {
			t.Fatal("cancelled dispatch not uncertain")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled helper survived")
	}
	if _, err := os.Stat(filepath.Join(root, "input-dispatched")); !os.IsNotExist(err) {
		t.Fatal("helper dispatched a new input after cancellation during preflight")
	}
	select {
	case <-driver.exited:
	default:
		t.Fatal("helper still alive")
	}
}

func TestHostIdleClosePreservesGracefulShutdown(t *testing.T) {
	driver, root := fixtureHost(t, map[string]any{})
	hostCall(t, driver, "bot_desktop_observe", map[string]any{})
	driver.Close()
	if _, err := os.Stat(filepath.Join(root, "shutdown-completed")); err != nil {
		t.Fatal("ordinary idle close skipped SDK cleanup", err)
	}
}

func TestHostResultsFitCaelisContentV1Receipt(t *testing.T) {
	// Match the pinned Host: text block bytes plus Go's JSON serialization of
	// outcome/receipt_id/structuredContent, including HTML and U+2028 escaping.
	check := func(t *testing.T, result api.ToolResult) {
		t.Helper()
		receipt, err := json.Marshal(map[string]any{"outcome": "succeeded", "receipt_id": "app-call-" + strings.Repeat("a", 64), "structuredContent": result.StructuredContent})
		if err != nil {
			t.Fatal(err)
		}
		size := len(receipt)
		for _, block := range result.Content {
			if block["type"] == "text" {
				size += len(block["text"])
			}
		}
		if size > 32<<10 {
			t.Fatalf("model-visible receipt is %d bytes, exceeding Caelis's 32768-byte limit", size)
		}
		frame, _ := json.Marshal(result)
		if len(frame) >= 512<<10 {
			t.Fatal("result exceeds pipe/MCP frame")
		}
	}
	for _, mode := range []string{"ascii", "escaping", "windows", "remaining", "image"} {
		t.Run(mode, func(t *testing.T) {
			config := map[string]any{"text": strings.Repeat("x", 18000)}
			switch mode {
			case "escaping":
				config["text"] = strings.Repeat("<>&\u2028\u2029界", 2000)
			case "windows":
				config["windows"], config["windowText"] = 80, strings.Repeat("界", 300)
			case "image":
				config["images"] = []any{map[string]any{"mimeType": "image/png", "dataBase64": base64.StdEncoding.EncodeToString(make([]byte, 256<<10))}}
			}
			driver, _ := fixtureHost(t, config)
			listed := hostCall(t, driver, "bot_desktop_observe", map[string]any{})
			check(t, listed)
			if mode == "windows" {
				if listed.StructuredContent["truncated"] != true {
					t.Fatal("truncated list not marked")
				}
				return
			}
			window := listed.StructuredContent["windows"].([]any)[0].(map[string]any)["window"]
			observed := hostCall(t, driver, "bot_desktop_observe", map[string]any{"window": window, "screenshot": mode == "image"})
			check(t, observed)
			if observed.StructuredContent["truncated"] != true {
				t.Fatal("receipt clipping was not disclosed")
			}
			if mode == "image" && (len(observed.Content) != 2 || observed.Content[1]["type"] != "image") {
				t.Fatal("budgeting removed optional image")
			}
			if mode != "remaining" {
				return
			}
			steps := []any{map[string]any{"op": "click", "target": "e2"}}
			for range 7 {
				steps = append(steps, map[string]any{"op": "type", "target": "e3", "text": strings.Repeat("z", 4000)})
			}
			performed := hostCall(t, driver, "bot_desktop_perform", map[string]any{"observation": observed.StructuredContent["observation"], "steps": steps})
			check(t, performed)
			if performed.StructuredContent["remainingCount"] != float64(7) || performed.StructuredContent["remainingTruncated"] != true {
				t.Fatal("unexecuted steps lost without explicit truncation")
			}
			if len(performed.StructuredContent["steps"].([]any)) != 1 {
				t.Fatal("lost dispatched step")
			}
			for _, step := range performed.StructuredContent["remaining"].([]any) {
				if step.(map[string]any)["text"] != strings.Repeat("z", 4000) {
					t.Fatal("pending input was silently shortened into a different action")
				}
			}
		})
	}
}
