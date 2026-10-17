package desktop

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/screeninput"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestWindowsPinGeometryPreservesImageAndMonitorBounds(t *testing.T) {
	work := image.Rect(-1920, 40, 0, 1080)
	for _, size := range []image.Point{image.Pt(430, 64), image.Pt(2560, 1440), image.Pt(8, 8), image.Pt(400, 1600)} {
		for _, scale := range []float64{0.01, 1, 1.08, 20} {
			r := fitWindowsPin(size.X, size.Y, scale, work, -3000, 2000)
			if r.Empty() || !r.In(work) {
				t.Fatal("pin escaped monitor", r)
			}
			if math.Abs(float64(r.Dx())/float64(size.X)-float64(r.Dy())/float64(size.Y)) > 1/float64(min(size.X, size.Y)) {
				t.Fatal("image distorted", r, size)
			}
		}
	}
	if r := fitWindowsPin(430, 64, 1, image.Rect(0, 0, 2560, 1440), 10, 10); r.Dx() != 430 || r.Dy() != 64 {
		t.Fatal("native-size pin gained padding", r)
	}
}

func TestWindowsCaptureReenableRestoresOriginalUnknownPresentation(t *testing.T) {
	w := &application.WebviewWindow{}
	old := &windowsCaptureDraft{window: w, ready: true, document: WindowsCaptureDocument{ID: "original", Outcome: "unknown", Presentation: 7}}
	c := &windowsCapture{driver: &windowsDriver{}, drafts: map[string]*windowsCaptureDraft{"original": old}}
	saved := &windowsCaptureDraft{document: WindowsCaptureDocument{ID: "original", Outcome: "unknown"}}
	if got := c.restoreDraft(saved); got != old {
		t.Fatal("restore replaced original receipt/UI owner")
	}
	if c.retainedPresentation(old) != nil {
		t.Fatal("disabled capture revealed")
	}
	c.enabled = true
	if c.retainedPresentation(old) != w || old.document.Presentation != 7 {
		t.Fatal("ready hidden capture did not resume")
	}
	if c.retainedPresentation(saved) != nil {
		t.Fatal("stale restore revealed")
	}
	old.ready = false
	if c.retainedPresentation(old) != nil {
		t.Fatal("unpainted capture revealed")
	}
}

func TestWindowsFastCaptureEncodingIsLossless(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 257, 129))
	for y := range 129 {
		for x := range 257 {
			im.SetNRGBA(x, y, color.NRGBA{byte(x), byte(y), byte(x * y), 255})
		}
	}
	var b bytes.Buffer
	if e := encodeWindowsCapture(&b, im); e != nil {
		t.Fatal(e)
	}
	decoded, e := png.Decode(bytes.NewReader(b.Bytes()))
	if e != nil {
		t.Fatal(e)
	}
	for y := range 129 {
		for x := range 257 {
			if decoded.At(x, y) != im.At(x, y) {
				a, b, c, d := decoded.At(x, y).RGBA()
				w, x, y, z := im.At(x, y).RGBA()
				if a != w || b != x || c != y || d != z {
					t.Fatal("pixels changed")
				}
			}
		}
	}
}

func TestWindowsCaptureRevealRequiresCurrentDecodedPresentation(t *testing.T) {
	w := &application.WebviewWindow{}
	d := &windowsCaptureDraft{window: w, document: WindowsCaptureDocument{ID: "owned", Presentation: 2}}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{"owned": d}}
	if c.claimPresentation("missing", 2) != nil || c.claimPresentation("owned", 1) != nil || d.ready {
		t.Fatal("stale acknowledgement revealed the replacement window")
	}
	if c.claimPresentation("owned", 2) != w || !d.ready || c.claimPresentation("owned", 2) != nil {
		t.Fatal("current presentation must reveal once")
	}
	d.ready = false
	c.enabled = false
	if c.claimPresentation("owned", 2) != nil || d.ready {
		t.Fatal("disabled capture was revealed")
	}
	c.enabled = true
	c.driver.stopped.Store(true)
	if c.claimPresentation("owned", 2) != nil {
		t.Fatal("shutdown capture was revealed")
	}
}

func TestWindowsCaptureWarmSlotCannotResolveAnotherOrClosedDocument(t *testing.T) {
	d := &windowsCaptureDraft{slot: "once", window: &application.WebviewWindow{}, document: WindowsCaptureDocument{ID: "owned"}}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{"owned": d}}
	if c.slotDocument("") != "" || c.slotDocument("other") != "" || c.slotDocument("once") != "owned" {
		t.Fatal("slot ownership mismatch")
	}
	c.enabled = false
	if c.slotDocument("once") != "" {
		t.Fatal("disabled slot resolved")
	}
	c.enabled = true
	d.slot = "replacement"
	if c.slotDocument("once") != "" {
		t.Fatal("consumed slot resolved a replacement")
	}
	d.window = nil
	if c.slotDocument("replacement") != "" {
		t.Fatal("closed window resolved")
	}
	delete(c.drafts, "owned")
	if c.slotDocument("replacement") != "" {
		t.Fatal("discarded document resolved")
	}
}

func TestWindowsCaptureFinishClosesOnlyCompletedOperations(t *testing.T) {
	w := &application.WebviewWindow{}
	c := &windowsCapture{enabled: true}
	for _, tc := range []struct {
		name, outcome    string
		completed, close bool
	}{
		{"copy or save succeeded", "draft", true, true},
		{"picker cancelled or export failed", "draft", false, false},
		{"pin remains visible", "pinned", false, false},
		{"submission still unknown", "unknown", false, false},
		{"acceptance raced export", "accepted", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &windowsCaptureDraft{window: w, exporting: true, document: WindowsCaptureDocument{Outcome: tc.outcome}}
			close, disabled := c.finishExport(d, tc.completed)
			if (close == w) != tc.close || d.exporting || disabled || d.document.Outcome != tc.outcome {
				t.Fatal("completion lost native lifecycle or submission state")
			}
		})
	}
}

func TestWindowsCaptureSaveCancellationKeepsEditableDocument(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	s.capture.preferences.Enabled = true
	pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	d := &windowsCaptureDraft{pixels: pixels, document: WindowsCaptureDocument{ID: "cancel-save", Outcome: "draft"}}
	native := &windowsDriver{service: s}
	native.capture = &windowsCapture{driver: native, enabled: true, drafts: map[string]*windowsCaptureDraft{d.document.ID: d}}
	s.native = native
	export := WindowsCaptureExport{ID: d.document.ID, Selection: screeninput.Rect{Width: 4, Height: 4}, PNG: base64.StdEncoding.EncodeToString(encoded.Bytes()), Action: "save"}
	native.capture.saveImage = func(_ *application.WebviewWindow, _ []byte) error { return errWindowsCaptureSaveCancelled }
	if err := s.ExportWindowsCapture(export); err != nil || d.exporting || d.document.Outcome != "draft" {
		t.Fatal("cancelled picker destroyed the editable capture", err)
	}
	failure := errors.New("disk full")
	native.capture.saveImage = func(_ *application.WebviewWindow, _ []byte) error { return failure }
	if err := s.ExportWindowsCapture(export); !errors.Is(err, failure) || d.exporting {
		t.Fatal("real save failure was suppressed", err)
	}
}

func TestWindowsCaptureRedactionContextAndReceiptGate(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	root := filepath.Join(t.TempDir(), "Captures")
	if e := s.configureCapture(root); e != nil {
		t.Fatal(e)
	}
	native := &windowsDriver{service: s}
	native.capture = &windowsCapture{driver: native, drafts: map[string]*windowsCaptureDraft{}}
	s.native = native
	pixels := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			pixels.Set(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	selection := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			selection.Set(x, y, color.Black)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, selection)
	id := "capture-redaction-fixture"
	d := &windowsCaptureDraft{pixels: pixels, snapshot: screeninput.Snapshot{Version: 1, ID: id, Source: "screen"}, document: WindowsCaptureDocument{ID: id, Outcome: "draft"}}
	native.capture.drafts[id] = d
	// Stop only dispatch in this fixture, retaining the exact preflight bytes.
	s.stopped = true
	export := WindowsCaptureExport{ID: id, Selection: screeninput.Rect{X: 12, Y: 12, Width: 24, Height: 24}, PNG: base64.StdEncoding.EncodeToString(b.Bytes()), Context: true, Action: "send"}
	if e := s.ExportWindowsCapture(export); e != nil {
		t.Fatal(e)
	}
	file, e := os.Open(filepath.Join(root, id, "context.jpg"))
	if e != nil {
		t.Fatal(e)
	}
	context, e := jpeg.Decode(file)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	r, g, bl, _ := context.At(24, 24).RGBA()
	if r > 5000 || g > 5000 || bl > 5000 {
		t.Fatal("original selected pixels leaked through redacted context")
	}
	r, _, _, _ = context.At(2, 2).RGBA()
	if r < 50000 {
		t.Fatal("context outside selection changed")
	}
	if e = s.ExportWindowsCapture(export); e == nil {
		t.Fatal("unresolved capture was exported again")
	}
	export.ID = "../escape"
	if e = s.ExportWindowsCapture(export); e == nil {
		t.Fatal("renderer created an unowned capture ID")
	}
}

func TestWindowsCaptureSaveAndPinEditKeepFlattenedPixels(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	s.capture.preferences.Enabled = true
	pixels := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	drawBlack := color.NRGBA{0, 0, 0, 255}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			pixels.Set(x, y, drawBlack)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	d := &windowsCaptureDraft{pixels: pixels, document: WindowsCaptureDocument{ID: "capture-save", Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()), Outcome: "pinned"}}
	native := &windowsDriver{service: s}
	native.capture = &windowsCapture{driver: native, enabled: true, drafts: map[string]*windowsCaptureDraft{"capture-save": d}}
	s.native = native
	destination := filepath.Join(t.TempDir(), "截图.png")
	if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	native.capture.saveImage = func(_ *application.WebviewWindow, data []byte) error {
		calls++
		return saveWindowsCapturePNG(destination, data)
	}
	// A forged renderer PNG must not replace the native-owned flattened pin.
	if err := s.ExportWindowsCapture(WindowsCaptureExport{ID: "capture-save", Action: "save", PNG: "forged"}); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(saved, encoded.Bytes()) || calls != 1 {
		t.Fatal("saved pin bytes changed", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(destination)); err != nil || len(entries) != 1 {
		t.Fatal("temporary save leaked", err)
	}
	if err := s.ExportWindowsCapture(WindowsCaptureExport{ID: "../unowned", Action: "save"}); err == nil || calls != 1 {
		t.Fatal("unowned capture reached picker")
	}
	if _, _, err = native.capture.editPin("capture-save"); err != nil {
		t.Fatal(err)
	}
	if d.pixels != pixels || d.document.Outcome != "draft" || d.document.ContextAvailable || d.snapshot.Source != "clipboard" {
		t.Fatal("pin edit restored source pixels or context")
	}
	d.document.Outcome = "unknown"
	if _, _, err = native.capture.editPin("capture-save"); err == nil {
		t.Fatal("unknown submission became editable")
	}
}

func TestWindowsCaptureRejectsDimensionSubstitution(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	s.capture.preferences.Enabled = true
	native := &windowsDriver{service: s}
	native.capture = &windowsCapture{driver: native, drafts: map[string]*windowsCaptureDraft{"capture-fixture": {pixels: image.NewNRGBA(image.Rect(0, 0, 8, 8)), document: WindowsCaptureDocument{Outcome: "draft"}}}}
	s.native = native
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	if e := s.ExportWindowsCapture(WindowsCaptureExport{ID: "capture-fixture", Selection: screeninput.Rect{Width: 3, Height: 3}, PNG: base64.StdEncoding.EncodeToString(b.Bytes()), Action: "send"}); e == nil {
		t.Fatal("substituted dimensions accepted")
	}
}

func TestWindowsCaptureReceiptBeforeWindowAndMetadataBounds(t *testing.T) {
	d := &windowsDriver{capture: &windowsCapture{enabled: true, drafts: map[string]*windowsCaptureDraft{"capture-pending-window": {document: WindowsCaptureDocument{Outcome: "unknown"}}}}}
	d.captureReceipt(map[string]string{"id": "capture-pending-window", "outcome": "draft", "message": "unavailable"})
	if got := d.capture.drafts["capture-pending-window"].document; got.Outcome != "draft" || got.Message != "unavailable" {
		t.Fatal("receipt lost before window presentation", got.Outcome, got.Message)
	}
	for _, limit := range []int{512, 2048} {
		got := boundedWindowsCaptureText(strings.Repeat("窗", 2049), limit)
		if len(got) > limit || len(got) < limit-2 || !utf8.ValidString(got) {
			t.Fatal("window metadata exceeds UTF-8 contract", limit, len(got))
		}
	}
}
