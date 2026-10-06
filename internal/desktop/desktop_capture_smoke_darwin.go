//go:build darwin && cgo

package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// host.Content exposes the model-facing fact projection ("known": value),
// not the SDK's wire Fact{Status, Value}. Decode that projection explicitly.
type captureSmokeObservation struct {
	Objects []struct {
		Ref  dw.Ref  `json:"ref"`
		Kind dw.Kind `json:"kind"`
		App  dw.Ref  `json:"app"`
		Name struct {
			Known string `json:"known"`
		} `json:"name"`
	} `json:"objects"`
	Coverage struct {
		Complete bool `json:"complete"`
	} `json:"coverage"`
}

func decodeCaptureSmokeResult(result map[string]any, target any) error {
	b, err := json.Marshal(result["result"])
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

// RunDesktopCaptureSmoke runs in the signed Dev app process, so macOS evaluates
// the actual app and its bundled helper. It accepts only a disposable window.
func RunDesktopCaptureSmoke(title, evidencePath, imagePath, oncePath string) error {
	if title == "" || evidencePath == "" || imagePath == "" || oncePath == "" {
		return errors.New("exact fixture title, private evidence/image paths and once marker required")
	}
	results := make(chan error, 1)
	app := application.New(application.Options{Name: "Caelis Bot desktop capture acceptance", Mac: application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory}})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			err := smokeDesktopCapture(title, evidencePath, imagePath, oncePath)
			if err != nil {
				log.Printf("DESKTOP CAPTURE E2E FAIL: %v", err)
			} else {
				log.Print("DESKTOP CAPTURE E2E PASS")
			}
			results <- err
			app.Quit()
		}()
	})
	if err := app.Run(); err != nil {
		return err
	}
	return <-results
}

func smokeDesktopCapture(title, evidencePath, imagePath, oncePath string) (err error) {
	proof := map[string]any{"fixtureTitle": title, "captureAttempts": 0, "imageAccepted": false}
	defer func() {
		b, e := json.MarshalIndent(proof, "", "  ")
		if e == nil {
			_ = os.WriteFile(evidencePath, b, 0600)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	bundle := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
	state := systemPermissionState("")
	proof["runningBundleMatchesMainBundle"] = filepath.Clean(state.AppPath) == bundle
	proof["runningBundleName"] = filepath.Base(bundle)
	if !proof["runningBundleMatchesMainBundle"].(bool) || proof["runningBundleName"] != "Caelis Bot Dev.app" {
		return errors.New("capture smoke is not running in the expected Dev app bundle")
	}
	for _, permission := range state.Permissions {
		if permission.ID == "screenCapture" {
			proof["appScreenCaptureStatus"] = permission.Status
		}
	}
	if proof["appScreenCaptureStatus"] != "authorized" {
		return errors.New("signed Dev app screen capture preflight is not authorized")
	}
	helper := filepath.Join(bundle, "Contents", "Resources", "DesktopWorld", "bin", "dtw")
	assets, err := os.MkdirTemp(filepath.Dir(evidencePath), "capture-assets-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(assets)
	c := desktopcontrol.New(helper, assets)
	defer c.Close()
	ctx, cancel := context.WithTimeout(desktopcontrol.WithTurn(context.Background(), "signed-rc2-capture"), 30*time.Second)
	defer cancel()
	call := func(id, op string, args map[string]any) (map[string]any, error) {
		body, _ := json.Marshal(map[string]any{"requestId": id, "args": args})
		out := c.CallTool(ctx, desktopcontrol.Prefix+op, body)
		if out.IsError {
			code := "unknown"
			if fault, ok := out.StructuredContent["error"].(map[string]any); ok {
				code, _ = fault["code"].(string)
			}
			proof[id+"Fault"] = code
			return nil, fmt.Errorf("%s: %s", id, code)
		}
		return out.StructuredContent, nil
	}
	inventory, err := call("capture-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	if err != nil {
		return err
	}
	var observed captureSmokeObservation
	if err := decodeCaptureSmokeResult(inventory, &observed); err != nil {
		return err
	}
	var appRef dw.Ref
	for _, object := range observed.Objects {
		if object.Kind == dw.KindWindow && object.Name.Known == title {
			if appRef != "" {
				return errors.New("ambiguous isolated capture window")
			}
			appRef = object.App
		}
	}
	if appRef == "" {
		return errors.New("isolated capture window absent")
	}
	var appName string
	for _, object := range observed.Objects {
		if object.Ref == appRef {
			appName = object.Name.Known
		}
	}
	if appName != "Caelis Desktop Control Fixture" {
		return errors.New("capture fixture application identity mismatch")
	}
	auth, _ := json.Marshal(map[string]string{"application": string(appRef), "name": appName, "purpose": "one isolated signed Dev window capture acceptance"})
	grant := c.CallTool(ctx, desktopcontrol.Prefix+"authorize", auth)
	if grant.IsError {
		return errors.New("isolated fixture app grant refused")
	}
	proof["appGrant"] = "accepted"
	windowResult, err := call("capture-windows", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{appRef}}, "projection": "capture_windows", "freshness": map[string]any{"mode": "refresh"}, "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 32, "max_output_bytes": 16384, "read_deadline_ms": 5000}})
	if err != nil {
		return err
	}
	var windows captureSmokeObservation
	if err := decodeCaptureSmokeResult(windowResult, &windows); err != nil {
		return err
	}
	var captureRef dw.Ref
	for _, object := range windows.Objects {
		if object.Kind == dw.KindWindow && object.App == appRef && object.Name.Known == title {
			if captureRef != "" {
				return errors.New("ambiguous capture identity")
			}
			captureRef = object.Ref
		}
	}
	if !windows.Coverage.Complete || captureRef == "" {
		return errors.New("unique fresh capture window unavailable")
	}
	marker, err := os.OpenFile(oncePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("capture for this fixture already attempted")
	}
	_, writeErr := marker.WriteString(title + "\n")
	syncErr := marker.Sync()
	closeErr := marker.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(writeErr, syncErr, closeErr)
	}
	proof["captureAttempts"] = 1
	args, _ := json.Marshal(map[string]any{"requestId": "signed-capture-once", "args": map[string]any{"kind": "window_content", "target": captureRef, "max_pixel_width": 640, "max_pixel_height": 480}})
	result := c.CallTool(ctx, desktopcontrol.Prefix+"capture", args)
	if result.IsError {
		code := "unknown"
		if fault, ok := result.StructuredContent["error"].(map[string]any); ok {
			code, _ = fault["code"].(string)
		}
		proof["captureFault"] = code
		return fmt.Errorf("capture: %s", code)
	}
	var capture dw.CaptureResult
	b, err := json.Marshal(result.StructuredContent["result"])
	if err != nil {
		return err
	}
	if err := protocol.Decode(b, &capture); err != nil {
		return err
	}
	if len(capture.Tiles) != 1 {
		return errors.New("capture did not return exactly one tile")
	}
	tile := capture.Tiles[0]
	if tile.Target != captureRef || tile.Kind != "window_content" || tile.DesktopFrame != "" || tile.ImageToDesktop != (dw.Transform2D{}) || tile.ImageToTarget.A <= 0 || tile.ImageToTarget.D <= 0 || tile.PixelWidth < 32 || tile.PixelHeight < 32 || tile.PixelWidth > 640 || tile.PixelHeight > 480 {
		return errors.New("capture target-local geometry invalid")
	}
	var pngBytes []byte
	for _, part := range result.Content {
		if part["type"] == "image" && part["mimeType"] == "image/png" {
			if pngBytes != nil {
				return errors.New("multiple capture images")
			}
			pngBytes, err = base64.StdEncoding.DecodeString(part["data"])
			if err != nil {
				return err
			}
		}
	}
	if len(pngBytes) == 0 || len(pngBytes) > 5<<20 {
		return errors.New("Bot image payload missing or oversized")
	}
	image, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return err
	}
	if image.Bounds().Dx() != tile.PixelWidth || image.Bounds().Dy() != tile.PixelHeight {
		return errors.New("PNG dimensions disagree with capture tile")
	}
	colors := map[[3]uint32]bool{}
	for y := 0; y < image.Bounds().Dy(); y += max(1, image.Bounds().Dy()/20) {
		for x := 0; x < image.Bounds().Dx(); x += max(1, image.Bounds().Dx()/20) {
			r, g, b, _ := image.At(x, y).RGBA()
			colors[[3]uint32{r >> 8, g >> 8, b >> 8}] = true
		}
	}
	if len(colors) < 3 {
		return errors.New("captured window image has insufficient visual variation")
	}
	if err := os.WriteFile(imagePath, pngBytes, 0600); err != nil {
		return err
	}
	proof["imageAccepted"] = true
	proof["pngBytes"] = len(pngBytes)
	proof["pixelWidth"] = tile.PixelWidth
	proof["pixelHeight"] = tile.PixelHeight
	proof["sampledColors"] = len(colors)
	proof["geometry"] = "one capture-window target; target-local positive transform; no desktop mapping; PNG dimensions match tile"
	return nil
}
