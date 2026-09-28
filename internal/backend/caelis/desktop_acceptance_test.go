package caelis

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

func TestContentCapabilityAndRestartCatalog(t *testing.T) {
	s := New(Options{Directory: filepath.Join(t.TempDir(), "binding")})
	h := &acceptanceTools{defs: []api.ToolDefinition{desktopcontrol.Definition()}}
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: h}); err != nil {
		t.Fatal(err)
	}
	if s.checkContentCapability(wire.ServerInfo{}) == nil {
		t.Fatal("old Host accepted content tool")
	}
	if err := s.checkContentCapability(wire.ServerInfo{Capabilities: []string{"application-tool-result-content-v1"}}); err != nil {
		t.Fatal(err)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][desktopcontrol.ToolName] {
		t.Fatal("callback format lost on restart")
	}
	if err := restored.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{defs: fixtureDefinitions("string")}}); err != nil {
		t.Fatal(err)
	}
	if err := restored.checkContentCapability(wire.ServerInfo{}); err != nil {
		t.Fatal("ordinary Bot now requires optional content capability")
	}
	if !restored.state.ContentCatalogs[s.profile.ToolsVersion][desktopcontrol.ToolName] {
		t.Fatal("disabled experiment lost old callback receipt format")
	}
}

// Optional live native fixture, still a synthetic provider. This validates the
// product-owned tool path without borrowing the daily model/store or worker tools.
func cuaHostAcceptance(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel) {
	node, script := os.Getenv("CAELIS_BOT_CUA_NODE"), os.Getenv("CAELIS_BOT_CUA_HOST")
	if node == "" || script == "" {
		t.Skip("requires explicit Cua SDK host and running native fixture")
	}
	driver, err := desktopcontrol.StartDriver(node, script)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	r, err := bot.NewForRuntime(filepath.Join(t.TempDir(), "resident.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.ConfigureDesktopControl(driver)
	var observed atomic.Pointer[api.ToolResult]
	workerDirectory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var delegationError atomic.Pointer[error]
	definitions := append(r.Definitions(), api.ToolDefinition{Name: "FixtureCuaDelegate", Description: "Test worker isolation", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)})
	h := &acceptanceTools{defs: definitions, call: func(c context.Context, name string, args json.RawMessage) api.ToolResult {
		if name == "FixtureCuaDelegate" {
			_, e := s.StartWork(c, api.WorkStart{ID: "cua-worker", Workspace: workerDirectory, Instructions: "Use only native worker capabilities.", TaskStart: api.TaskStart{RequestID: "cua-worker", Title: "Desktop scope probe", Prompt: "CASE_CUA_WORKER", Workspace: workerDirectory}})
			if e != nil {
				delegationError.Store(&e)
			}
			return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "Worker scope probe requested"}}, IsError: e != nil}
		}
		out := r.CallTool(c, name, args)
		if name == "bot_desktop_observe" || name == "bot_desktop_perform" {
			observed.Store(&out)
		}
		return out
	}}
	if err = s.ConfigureBotTools(&api.ToolConnection{Host: h}); err != nil {
		t.Fatal(err)
	}
	current, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateConfiguration(ctx, "cua-fixture-tools", string(current.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}); err != nil {
		t.Fatal(err)
	}
	model.set("CASE_CUA_OBSERVE", modelStep{Name: "bot_desktop_observe", Args: map[string]any{}}, modelStep{Reply: "STRUCTURED_DESKTOP_RECEIVED"})
	submitAcceptance(t, ctx, s, "CASE_CUA_OBSERVE")
	if observed.Load().IsError {
		t.Fatal("Cua observation failed", observed.Load().Content)
	}
	state := observed.Load().StructuredContent
	filter := func(state map[string]any) map[string]any {
		t.Helper()
		var found map[string]any
		for _, value := range state["targets"].([]any) {
			v := value.(map[string]any)
			if v["name"] == "Only incomplete" {
				if found != nil {
					t.Fatal("ambiguous filter")
				}
				found = v
			}
		}
		if found == nil {
			t.Fatal("filter missing")
		}
		return found
	}
	before := filter(state)["value"]
	for i, key := range []string{"CASE_CUA_PERFORM", "CASE_CUA_RESTORE"} {
		model.set(key, modelStep{Name: "bot_desktop_perform", Args: map[string]any{"observation": state["observation"], "steps": []any{map[string]any{"op": "click", "target": filter(state)["target"]}}}}, modelStep{Reply: "STRUCTURED_EFFECT_RECEIVED"})
		submitAcceptance(t, ctx, s, key)
		if observed.Load().IsError {
			t.Fatal("Cua action failed", observed.Load().Content)
		}
		state = observed.Load().StructuredContent["observation"].(map[string]any)
		if (filter(state)["value"] == before) != (i == 1) {
			t.Fatal("actual UI state not changed/restored")
		}
	}
	requests := model.seen("CASE_CUA_OBSERVE")
	if len(requests) != 2 {
		t.Fatal("missing Cua observation continuation")
	}
	beforeRaw, _ := json.Marshal(requests[0])
	raw, _ := json.Marshal(requests[1])
	// Earlier tests deliberately placed images in the same resident history.
	// Compare this tool's continuation against its own preceding request.
	if strings.Count(string(raw), "data:image/") != strings.Count(string(beforeRaw), "data:image/") || !strings.Contains(string(raw), "Only incomplete") {
		t.Fatal("structured path lost controls or used image")
	}
	model.set("CASE_CUA_WORKER", modelStep{Reply: "NATIVE_WORKER_SCOPE"})
	model.set("CASE_CUA_DELEGATE", modelStep{Name: "FixtureCuaDelegate", Args: map[string]any{}}, modelStep{Reply: "WORKER_REQUESTED"})
	submitAcceptance(t, ctx, s, "CASE_CUA_DELEGATE")
	if e := delegationError.Load(); e != nil {
		t.Fatal(*e)
	}
	waitAcceptance(t, ctx, func() bool {
		work, _ := s.ReadWork(ctx, "cua-worker")
		return work.Status == "completed"
	})
	workerRequests := model.seen("CASE_CUA_WORKER")
	if len(workerRequests) != 1 {
		t.Fatal("missing native worker request")
	}
	workerPayload, _ := json.Marshal(workerRequests[0])
	for _, private := range []string{"bot_desktop_", "Only incomplete", "desktop-observation.md", "CAELIS_BOT_CUA_"} {
		if strings.Contains(string(workerPayload), private) {
			t.Fatal("desktop capability/context leaked to worker", private)
		}
	}
	t.Log("real Cua SDK + native AX fixture -> resident Bot tools -> Caelis callback -> provider text/structured feedback and worker isolation passed; zero screenshots; synthetic model")
}

func desktopHostAcceptance(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if !slices.Contains(s.info.Capabilities, "application-tool-result-content-v1") {
		t.Skip("external Host predates content-v1")
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 24)))
	frame := desktopcontrol.Frame{World: desktopcontrol.World{Display: "fixture-display", Frame: desktopcontrol.Rect{Width: 1280, Height: 960}, WorkArea: desktopcontrol.Rect{Width: 1280, Height: 920}, Actor: desktopcontrol.Rect{X: 900, Y: 20, Width: 180, Height: 240}}, Image: encoded.Bytes(), MIME: "image/png", CapturedAt: time.Now()}
	r, err := bot.NewForRuntime(filepath.Join(t.TempDir(), "resident.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfigureDesktop(func(context.Context) (desktopcontrol.Frame, error) { return frame, nil })
	r.Start(s)
	defer r.Close()
	if err = s.ConfigureBotTools(&api.ToolConnection{Host: r, Instructions: "Desktop observation acceptance"}); err != nil {
		t.Fatal(err)
	}
	current, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateConfiguration(ctx, "desktop-poc-tools", string(current.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools})
	if err != nil {
		t.Fatal(err)
	}
	model.set("CASE_DESKTOP_OBSERVE", modelStep{Name: desktopcontrol.ToolName, Args: map[string]any{}}, modelStep{Reply: "DESKTOP_OBSERVATION_RECEIVED"})
	submitAcceptance(t, ctx, s, "CASE_DESKTOP_OBSERVE")
	requests := model.seen("CASE_DESKTOP_OBSERVE")
	if len(requests) != 2 {
		t.Fatal("missing model continuation", len(requests))
	}
	raw, _ := json.Marshal(requests[1])
	if !strings.Contains(string(raw), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(frame.Image)) || !strings.Contains(string(raw), "fixture-display") || !strings.Contains(string(raw), "observation") {
		t.Fatal("model request lost image bytes or matching spatial metadata")
	}
	// Inspect the provider request shape, not a string-only JSON receipt that
	// happens to contain base64. Caelis must send an actual image input part.
	if !strings.Contains(string(raw), `"type":"image_url"`) && !strings.Contains(string(raw), `"type":"input_image"`) {
		t.Fatal("image was downgraded to text")
	}
	t.Log("resident tool -> native Host content-v1 -> provider image + geometry verified; synthetic model, no visual grounding claim")
}
