package desktopcontrol

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func desktopTestPNG(t *testing.T, noise bool) []byte {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	random := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			c := color.RGBA{245, 245, 245, 255}
			if noise {
				c = color.RGBA{uint8(random.Uint32()), uint8(random.Uint32()), uint8(random.Uint32()), 255}
			} else if x%11 < 5 && y%20 < 10 {
				c = color.RGBA{20, 25, 30, 255}
			}
			m.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func imageResult(data []byte) api.ToolResult {
	return api.ToolResult{Content: []map[string]string{{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(data)}},
		StructuredContent: map[string]any{"imageWidth": float64(1000), "imageHeight": float64(600)}}
}

func TestDesktopImagesPreserveGeometryAndPreferLosslessCompression(t *testing.T) {
	for _, noise := range []bool{false, true} {
		data := desktopTestPNG(t, noise)
		if len(data) <= MaxImageBytes {
			t.Fatal("fixture must exceed the public image limit")
		}
		result := imageResult(data)
		if noise {
			result.StructuredContent = map[string]any{"observation": result.StructuredContent}
		}
		if err := boundImages(&result); err != nil {
			t.Fatal(err)
		}
		encoded, err := base64.StdEncoding.DecodeString(result.Content[0]["data"])
		if err != nil || len(encoded) > MaxImageBytes {
			t.Fatal("image exceeds public bound", err)
		}
		decoded, kind, err := image.Decode(bytes.NewReader(encoded))
		if err != nil || decoded.Bounds() != image.Rect(0, 0, 1000, 600) {
			t.Fatal("compression changed coordinate geometry", err)
		}
		if !noise {
			if kind != "png" {
				t.Fatal("text-like image needlessly became lossy")
			}
			original, _, _ := image.Decode(bytes.NewReader(data))
			for _, p := range []image.Point{{2, 3}, {17, 45}, {999, 599}} {
				if color.RGBAModel.Convert(decoded.At(p.X, p.Y)) != color.RGBAModel.Convert(original.At(p.X, p.Y)) {
					t.Fatal("lossless path changed text pixels")
				}
			}
		} else if kind != "jpeg" {
			t.Fatal("high entropy image did not exercise same-size JPEG fallback")
		}
		previous := result.Content[0]["data"]
		if err := boundImages(&result); err != nil || previous != result.Content[0]["data"] {
			t.Fatal("an image already under the limit was re-encoded", err)
		}
	}
}

func TestDesktopImageGeometryMismatchOrCorruptionRefusesInsteadOfInventingCoordinates(t *testing.T) {
	data := desktopTestPNG(t, false)
	for _, change := range []func(*api.ToolResult){
		func(r *api.ToolResult) { r.StructuredContent["imageWidth"] = float64(999) },
		func(r *api.ToolResult) { r.Content[0]["data"] = "invalid" },
		func(r *api.ToolResult) { r.Content[0]["data"] = base64.StdEncoding.EncodeToString(data[:100]) },
		func(r *api.ToolResult) { r.Content[0]["mimeType"] = "image/jpeg" },
		func(r *api.ToolResult) { r.Content = append(r.Content, r.Content[0]) },
	} {
		result := imageResult(data)
		change(&result)
		if err := boundImages(&result); err == nil {
			t.Fatal("invalid image was accepted")
		}
	}
}
