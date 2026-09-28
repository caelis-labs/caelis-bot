package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

type visionEngine struct {
	*fakeEngine
	state string
}

func (e *visionEngine) ImageInput(context.Context) (api.ImageInputCapability, error) {
	return api.ImageInputCapability{State: e.state}, nil
}
func TestDesktopExperimentGatingAndMCPImageRoundTrip(t *testing.T) {
	r, f, _ := fixture(t)
	if out := r.CallTool(t.Context(), desktopcontrol.ToolName, json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("default off bypassed")
	}
	var imageBytes bytes.Buffer
	_ = png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	count := 0
	r.ConfigureDesktop(func(context.Context) (desktopcontrol.Frame, error) {
		count++
		return desktopcontrol.Frame{World: desktopcontrol.World{Display: "one", Frame: desktopcontrol.Rect{Width: 100, Height: 100}, WorkArea: desktopcontrol.Rect{Width: 100, Height: 90}, Actor: desktopcontrol.Rect{Width: 18, Height: 24}}, MIME: "image/png", Image: imageBytes.Bytes(), CapturedAt: time.Now()}, nil
	})
	e := &visionEngine{f, "unknown"}
	r.engine = e
	for _, state := range []string{"unknown", "unsupported"} {
		e.state = state
		if out := r.CallTool(t.Context(), desktopcontrol.ToolName, json.RawMessage(`{}`)); !out.IsError || count != 0 {
			t.Fatal("unsupported model captured screen")
		}
	}
	e.state = "supported"
	for _, args := range []string{`null`, `{"click":true}`} {
		if out := r.CallTool(t.Context(), desktopcontrol.ToolName, json.RawMessage(args)); !out.IsError || count != 0 {
			t.Fatal("extra arguments accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out := r.CallTool(ctx, desktopcontrol.ToolName, json.RawMessage(`{}`)); !out.IsError || count != 0 {
		t.Fatal("cancelled capture")
	}
	b, err := Serve(r)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	config := b.Config("owned-binary")
	for key, value := range config.Env {
		t.Setenv(key, value)
	}
	var out bytes.Buffer
	if err = RunStdio(strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"bot_desktop_capture\",\"arguments\":{}}}\n"), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], desktopcontrol.ToolName) || strings.Contains(lines[1], "result_format") {
		t.Fatal("incorrect MCP discovery")
	}
	var response struct {
		Result api.ToolResult `json:"result"`
	}
	if json.Unmarshal([]byte(lines[2]), &response) != nil || response.Result.IsError || len(response.Result.Content) != 2 || response.Result.Content[1]["type"] != "image" || response.Result.StructuredContent["observation"] == nil || count != 1 {
		t.Fatal("image/metadata did not reach MCP client")
	}
}

type semanticFixture struct{ calls int }

func (*semanticFixture) Definitions() []api.ToolDefinition {
	return desktopcontrol.SemanticDefinitions()
}
func (s *semanticFixture) CallTool(context.Context, string, json.RawMessage) api.ToolResult {
	s.calls++
	return api.ToolResult{Content: []map[string]string{{"type": "text", "text": "fixture"}}}
}

func TestSemanticCaptureRequiresModelSupportButMetadataDoesNot(t *testing.T) {
	r, f, _ := fixture(t)
	provider := &semanticFixture{}
	r.ConfigureDesktopControl(provider)
	e := &visionEngine{f, "unknown"}
	r.engine = e
	if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); out.IsError || provider.calls != 1 {
		t.Fatal("metadata depends on vision")
	}
	for _, state := range []string{"unknown", "unsupported"} {
		e.state = state
		if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{"window":"fixture","screenshot":true}`)); !out.IsError || provider.calls != 1 {
			t.Fatal("unsupported image reached provider")
		}
	}
	e.state = "supported"
	if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{"window":"fixture","screenshot":true}`)); out.IsError || provider.calls != 2 {
		t.Fatal("supported image was blocked")
	}
}

type blockingDesktop struct {
	entered   chan struct{}
	cancelled chan struct{}
}

func (*blockingDesktop) Definitions() []api.ToolDefinition {
	return desktopcontrol.SemanticDefinitions()
}
func (d *blockingDesktop) CallTool(ctx context.Context, _ string, _ json.RawMessage) api.ToolResult {
	close(d.entered)
	<-ctx.Done()
	close(d.cancelled)
	return api.ToolResult{IsError: true}
}
func TestResidentStopCancelsInFlightMCPDesktopCallAndFencesLaterCalls(t *testing.T) {
	r, _, _ := fixture(t)
	d := &blockingDesktop{make(chan struct{}), make(chan struct{})}
	r.ConfigureDesktopControl(d)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.CallTool(context.Background(), "bot_desktop_perform", json.RawMessage(`{}`))
	}()
	<-d.entered
	r.StopDesktopTurn()
	select {
	case <-d.cancelled:
	case <-time.After(time.Second):
		t.Fatal("resident interrupt did not cancel desktop input")
	}
	<-done
	if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("stopped turn can observe again")
	}
	provider := &semanticFixture{}
	r.desktopControl = provider
	r.BeginDesktopTurn()
	if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); out.IsError || provider.calls != 1 {
		t.Fatal("new turn could not observe")
	}
	r.Stop()
	r.BeginDesktopTurn()
	if out := r.CallTool(t.Context(), "bot_desktop_observe", json.RawMessage(`{}`)); !out.IsError {
		t.Fatal("stopped runtime reactivated")
	}
}
