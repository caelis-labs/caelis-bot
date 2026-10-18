package desktop

import (
	"encoding/binary"
	"errors"
	"image"
	"net/http"
	"strings"
)

// The interactive preview is lossless, uncompressed BGR. Do not put PNG
// compression or a base64/JSON copy of every screen pixel on F1's critical path.
// PNG remains the format for confirmed exports and durable capture receipts.
func windowsCapturePreview(im *image.NRGBA) ([]byte, error) {
	w, h := im.Bounds().Dx(), im.Bounds().Dy()
	if w < 1 || h < 1 || int64(w)*int64(h) > 16<<20 {
		return nil, errors.New("capture preview dimensions unavailable")
	}
	stride := (w*3 + 3) &^ 3
	b := make([]byte, 54+stride*h)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[2:], uint32(len(b)))
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	for y := 0; y < h; y++ {
		src := im.Pix[y*im.Stride:]
		dst := b[54+(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			dst[x*3], dst[x*3+1], dst[x*3+2] = src[x*4+2], src[x*4+1], src[x*4]
		}
	}
	return b, nil
}

// This is a Wails virtual-origin asset route, not a listening network server.
// Only a currently owned random capture ID can resolve bytes; no disk paths,
// caching, directory enumeration or caller-selected acquisition is involved.
func (s *Service) windowsCapturePreviewHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/capture-preview/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, prefix)
		s.mu.Lock()
		d, ok := s.native.(*windowsDriver)
		s.mu.Unlock()
		if !ok || d.capture == nil {
			http.NotFound(w, r)
			return
		}
		c := d.capture
		c.mu.Lock()
		var data []byte
		if draft := c.drafts[id]; draft != nil && !draft.dismissed && c.enabled {
			data = draft.preview
		}
		c.mu.Unlock()
		if len(data) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/bmp")
		_, _ = w.Write(data)
	})
}
