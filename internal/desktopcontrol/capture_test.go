package desktopcontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestCaptureReadsOnlyOwnedImageAndReconcileDoesNotAttachPixels(t *testing.T) {
	c := New("", t.TempDir())
	data := desktopTestPNG(t, false)
	path := filepath.Join(c.assets, "capture.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	saved := struct {
		Capture dw.CaptureResult
		Files   []map[string]any
	}{
		Capture: dw.CaptureResult{ExpiresAt: time.Now().Add(time.Minute), Tiles: []dw.CaptureTile{{Asset: "image", Kind: "window_content", Target: "capture-window", ImageToTarget: dw.Transform2D{A: 0.5, D: 0.5}, PixelWidth: 1000, PixelHeight: 600}}},
		Files:   []map[string]any{{"asset": "image", "path": path, "bytes": len(data)}},
	}
	b, _ := protocol.Marshal(saved)
	reply := host.Reply{Result: b}
	result := c.project(reply, "capture-request", "capture")
	if result.IsError || len(result.Content) != 2 || result.Content[1]["type"] != "image" {
		t.Fatal("capture lost image", result.IsError)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	if strings.Contains(string(raw), path) {
		t.Fatal("private path exposed")
	}
	var envelope struct{ Result json.RawMessage }
	json.Unmarshal(raw, &envelope)
	var projected dw.CaptureResult
	if protocol.Decode(envelope.Result, &projected) != nil || projected.Tiles[0].ImageToTarget.A != 0.5 || projected.Tiles[0].DesktopFrame != "" || projected.Tiles[0].ImageToDesktop != (dw.Transform2D{}) {
		t.Fatal("target-local transform changed or gained desktop input authority", string(raw))
	}
	recovered := c.project(reply, "capture-request", "reconcile")
	if recovered.IsError || len(recovered.Content) != 1 {
		t.Fatal("recovery emitted pixels")
	}
	saved.Files[0]["path"] = filepath.Join(c.assets, "..", "outside.png")
	b, _ = protocol.Marshal(saved)
	if !c.project(host.Reply{Result: b}, "capture-request", "capture").IsError {
		t.Fatal("unowned capture accepted")
	}
}
