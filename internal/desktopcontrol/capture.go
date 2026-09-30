package desktopcontrol

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

// Capture is the sole image-producing call. Recovery returns metadata, never
// silently recaptures pixels. Private helper paths do not enter model context.
func (c *Controller) project(reply host.Reply, id, op string) api.ToolResult {
	var saved struct {
		Capture dw.CaptureResult
		Files   []struct {
			Asset dw.AssetID
			Path  string
			Bytes int
		}
	}
	if protocol.Decode(reply.Result, &saved) != nil || len(saved.Files) == 0 {
		return content(reply, id)
	}
	public, _ := protocol.Marshal(saved.Capture)
	reply.Result = public
	out := content(reply, id)
	if op != "capture" || out.IsError {
		return out
	}
	var imageErr error
	if len(saved.Files) != 1 || len(saved.Capture.Tiles) != 1 {
		imageErr = errors.New("capture contains multiple tiles; request one smaller visible region")
	}
	if !time.Now().Before(saved.Capture.ExpiresAt) {
		imageErr = errors.New("capture asset expired; no automatic recapture")
	}
	if imageErr == nil {
		file, tile := saved.Files[0], saved.Capture.Tiles[0]
		rel, err := filepath.Rel(c.assets, file.Path)
		if err == nil && file.Asset == tile.Asset && file.Bytes > 0 && file.Bytes <= 5<<20 && filepath.IsLocal(rel) {
			var root *os.Root
			root, err = os.OpenRoot(c.assets)
			if err == nil {
				var f *os.File
				f, err = root.Open(rel)
				if err == nil {
					var data []byte
					data, err = io.ReadAll(io.LimitReader(f, 5<<20+1))
					f.Close()
					if err == nil && len(data) != file.Bytes {
						err = errors.New("capture asset size mismatch")
					}
					if err == nil {
						img := api.ToolResult{Content: []map[string]string{{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(data)}}, StructuredContent: map[string]any{"imageWidth": float64(tile.PixelWidth), "imageHeight": float64(tile.PixelHeight)}}
						err = boundImages(&img)
						if err == nil {
							out.Content = append(out.Content, img.Content...)
						}
					}
				}
				root.Close()
			}
		} else {
			err = errors.New("capture asset is not a valid host-owned image")
		}
		imageErr = err
	}
	if imageErr != nil {
		reply.Error = &dw.Fault{Code: "capture_image_unavailable", Message: imageErr.Error(), RetryClass: "read_only"}
		return content(reply, id)
	}
	return out
}
