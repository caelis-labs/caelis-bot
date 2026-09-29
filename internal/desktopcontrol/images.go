package desktopcontrol

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Keep the native capture's exact pixel dimensions: Cua's immutable capture
// receipt and all model-selected coordinates refer to this image. Try lossless
// PNG first, then the highest JPEG quality that fits the public content limit.
// Failure aborts the helper so an image not delivered to the model can never
// authorize later visual input.
func boundImages(result *api.ToolResult) error {
	count := 0
	for _, block := range result.Content {
		if block["type"] != "image" {
			continue
		}
		count++
		if count > 1 || len(block["data"]) > base64.StdEncoding.EncodedLen(5<<20) {
			return errors.New("invalid desktop image count or size")
		}
		data, err := base64.StdEncoding.DecodeString(block["data"])
		if err != nil {
			return errors.New("invalid desktop image encoding")
		}
		config, kind, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (kind != "png" && kind != "jpeg") || block["mimeType"] != "image/"+kind ||
			config.Width < 1 || config.Height < 1 || config.Width > 1000 || config.Height > 1000 {
			return errors.New("invalid desktop image dimensions")
		}
		observation := result.StructuredContent
		if nested, ok := observation["observation"].(map[string]any); ok {
			observation = nested
		}
		if observation["imageWidth"] != float64(config.Width) || observation["imageHeight"] != float64(config.Height) {
			return errors.New("desktop image geometry mismatch")
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return errors.New("invalid desktop image")
		}
		if len(data) <= MaxImageBytes {
			continue
		}
		var encoded bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&encoded, decoded); err != nil {
			return errors.New("desktop PNG encoding failed")
		}
		mime := "image/png"
		if encoded.Len() > MaxImageBytes {
			mime = "image/jpeg"
			// A white background avoids black text/fringes when flattening alpha.
			opaque := image.NewRGBA(decoded.Bounds())
			for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
				for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
					r, g, b, a := decoded.At(x, y).RGBA()
					i := opaque.PixOffset(x, y)
					opaque.Pix[i], opaque.Pix[i+1], opaque.Pix[i+2], opaque.Pix[i+3] =
						uint8((r+0xffff-a)>>8), uint8((g+0xffff-a)>>8), uint8((b+0xffff-a)>>8), 255
				}
			}
			// Search rather than dropping through coarse quality levels. Retain
			// the highest quality fitting the budget, including quality 100.
			var best []byte
			for low, high := 1, 100; low <= high; {
				quality := (low + high) / 2
				var candidate bytes.Buffer
				if err := jpeg.Encode(&candidate, opaque, &jpeg.Options{Quality: quality}); err != nil {
					return errors.New("desktop JPEG encoding failed")
				}
				if candidate.Len() <= MaxImageBytes {
					best = candidate.Bytes()
					low = quality + 1
				} else {
					high = quality - 1
				}
			}
			if best == nil {
				return errors.New("desktop image cannot fit without resizing")
			}
			encoded = *bytes.NewBuffer(best)
		}
		if encoded.Len() > MaxImageBytes {
			return errors.New("desktop image cannot fit without resizing")
		}
		block["data"], block["mimeType"] = base64.StdEncoding.EncodeToString(encoded.Bytes()), mime
	}
	return nil
}
