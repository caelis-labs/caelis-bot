package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWindowsCaptureBitmapPreviewIsLosslessWithPaddedRows(t *testing.T) {
	// Include a non-zero origin and a row stride inherited from a larger image.
	parent := image.NewNRGBA(image.Rect(0, 0, 260, 132))
	im := parent.SubImage(image.Rect(2, 2, 259, 131)).(*image.NRGBA)
	for y := 2; y < 131; y++ {
		for x := 2; x < 259; x++ {
			im.SetNRGBA(x, y, color.NRGBA{byte(x), byte(y), byte(x * y), 255})
		}
	}
	b, err := windowsCapturePreview(im)
	if err != nil {
		t.Fatal(err)
	}
	pngBytes, err := dibPNG(b[14:])
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < im.Bounds().Dy(); y++ {
		for x := 0; x < im.Bounds().Dx(); x++ {
			r, g, blue, a := decoded.At(x, y).RGBA()
			wr, wg, wb, wa := im.At(x+2, y+2).RGBA()
			if r != wr || g != wg || blue != wb || a != wa {
				t.Fatal("preview changed a pixel", x, y)
			}
		}
	}
	if _, err := windowsCapturePreview(image.NewNRGBA(image.Rectangle{})); err == nil {
		t.Fatal("empty image accepted")
	}
}

func TestWindowsCapturePreviewRouteExpiresWithOriginalDocument(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &windowsCaptureDraft{preview: []byte("owned pixels")}
	c := &windowsCapture{enabled: true, drafts: map[string]*windowsCaptureDraft{"random-original": d}}
	s.native = &windowsDriver{capture: c}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := s.windowsCapturePreviewHandler(next)
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(method, path, nil))
		return r
	}
	if r := request("GET", "/capture-preview/random-original"); r.Code != 200 || r.Body.String() != "owned pixels" || r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("Content-Type") != "image/bmp" {
		t.Fatal("owned preview unavailable or cacheable", r.Code)
	}
	for _, path := range []string{"/capture-preview/unknown", "/capture-preview/../random-original", "/capture-preview/"} {
		if r := request("GET", path); r.Code != 404 {
			t.Fatal("unowned preview exposed", path)
		}
	}
	if r := request("POST", "/capture-preview/random-original"); r.Code != 405 {
		t.Fatal("write method accepted")
	}
	d.dismissed = true
	if r := request("GET", "/capture-preview/random-original"); r.Code != 404 {
		t.Fatal("dismissed pixels still exposed")
	}
	d.dismissed = false
	c.enabled = false
	if r := request("GET", "/capture-preview/random-original"); r.Code != 404 {
		t.Fatal("disabled feature exposed pixels")
	}
	c.enabled = true
	delete(c.drafts, "random-original")
	if r := request("GET", "/capture-preview/random-original"); r.Code != 404 {
		t.Fatal("recycled editor exposed its old document")
	}
	if r := request("GET", "/assets/index.js"); r.Code != 204 {
		t.Fatal("preview handler swallowed ordinary assets")
	}
}

func BenchmarkWindowsCapturePreview1080p(b *testing.B) {
	im := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
	for i := 0; i < len(im.Pix); i += 4 {
		im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = byte(i/4), byte(i/7680), byte(i/7), 255
	}
	b.Run("BMP", func(b *testing.B) {
		for b.Loop() {
			_, _ = windowsCapturePreview(im)
		}
	})
	b.Run("PNG", func(b *testing.B) {
		for b.Loop() {
			var out bytes.Buffer
			_ = encodeWindowsCapture(&out, im)
		}
	})
}
