package desktop

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/screeninput"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestWindowsCaptureToggleCancelsAcquisitionAndLatePaint(t *testing.T) {
	c := &windowsCapture{enabled: true, driver: &windowsDriver{}, drafts: map[string]*windowsCaptureDraft{}}
	a := &windowsCaptureAcquisition{screen: true}
	c.acquisition = a
	if handled, w := c.toggleScreenLocked(); !handled || w != nil || !a.cancelled {
		t.Fatal("second F1 did not cancel pending screen pixels")
	}
	d := &windowsCaptureDraft{document: WindowsCaptureDocument{ID: "screen", Outcome: "draft", Presentation: 1}}
	if c.admitAcquisitionLocked(a, d) || len(c.drafts) != 0 {
		t.Fatal("cancelled pixels were admitted after F1")
	}
	a = &windowsCaptureAcquisition{screen: true}
	c.acquisition = a
	if !c.admitAcquisitionLocked(a, d) || c.acquisition != nil || c.editorID != "screen" {
		t.Fatal("a fresh acquisition did not replace the cancelled operation")
	}
	w := &application.WebviewWindow{}
	d.window = w
	if handled, got := c.toggleScreenLocked(); !handled || got != w || !d.dismissed {
		t.Fatal("second F1 did not dismiss its own editor")
	}
	if c.claimPresentation("screen", 1) != nil || c.retainedPresentation(d) != nil {
		t.Fatal("late renderer paint revived a dismissed editor")
	}
	if handled, got := c.toggleScreenLocked(); !handled || got != w {
		t.Fatal("F1 during native close started an overlapping capture")
	}
}

func TestWindowsCaptureTogglePreservesPinsAndUncertainExports(t *testing.T) {
	w := &application.WebviewWindow{}
	for _, outcome := range []string{"draft", "unknown"} {
		d := &windowsCaptureDraft{window: w, exporting: true, document: WindowsCaptureDocument{ID: "original", Outcome: outcome}}
		pin := &windowsCaptureDraft{document: WindowsCaptureDocument{ID: "pin", Outcome: "pinned"}}
		c := &windowsCapture{editorID: "original", drafts: map[string]*windowsCaptureDraft{"original": d, "pin": pin}}
		c.toggleScreenLocked()
		if c.drafts["original"] != d || !d.exporting || d.document.Outcome != outcome || pin.dismissed {
			t.Fatal("cancellation lost uncertain effects or dismissed a separate pin")
		}
	}
	c := &windowsCapture{editorID: "unpainted", drafts: map[string]*windowsCaptureDraft{"unpainted": {document: WindowsCaptureDocument{ID: "unpainted", Outcome: "draft"}}}}
	if handled, _ := c.toggleScreenLocked(); !handled || len(c.drafts) != 0 || c.editorID != "" {
		t.Fatal("cancelled unpainted document leaked a capture slot")
	}
}

func TestWindowsCapturePinRetainsSelectionOnSecondaryDisplayAndAfterResize(t *testing.T) {
	selection := image.Rect(240, 180, 640, 380)
	screen := image.Rect(-1920, -200, 0, 880)
	want := image.Rect(-1680, -20, -1280, 180)
	if got := windowsPinSelectionBounds(selection, screen, image.Rectangle{}, 1920, 1080); got != want {
		t.Fatalf("pin moved away from selected screen pixels: %v", got)
	}
	// Re-edit the already flattened image after moving and scaling its pin.
	pin := image.Rect(-1200, 60, -1000, 160)
	if got := windowsPinSelectionBounds(image.Rect(100, 40, 300, 160), image.Rectangle{}, pin, 400, 200); got != image.Rect(-1150, 80, -1050, 140) {
		t.Fatalf("re-pinned selection lost its current display geometry: %v", got)
	}
}

func TestWindowsCaptureSlowRendererDoesNotBlockReadyOrClose(t *testing.T) {
	w := &application.WebviewWindow{}
	d := &windowsCaptureDraft{window: w, document: WindowsCaptureDocument{ID: "capture", Presentation: 1, Outcome: "unknown"}}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{"capture": d}}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		c.updateAvailability("supported", func(*application.WebviewWindow) { close(entered); <-release })
		close(finished)
	}()
	defer func() { close(release); <-finished }()
	<-entered
	ready := make(chan *application.WebviewWindow, 1)
	go func() { ready <- c.claimPresentation("capture", 1) }()
	select {
	case got := <-ready:
		if got != w || d.document.ImageCapability != "supported" || d.document.Outcome != "unknown" {
			t.Fatal("capability refresh changed presentation or uncertain receipt")
		}
	case <-time.After(time.Second):
		t.Fatal("slow renderer held the capture lock and blocked the UI thread")
	}
	// A close/disable can still preserve this uncertain request while delivery
	// is stalled. Re-enable must restore the same already decoded presentation.
	c.mu.Lock()
	c.enabled = false
	c.mu.Unlock()
	if c.retainedPresentation(d) != nil {
		t.Fatal("disabled presentation remained visible")
	}
	c.mu.Lock()
	c.enabled = true
	c.mu.Unlock()
	if c.retainedPresentation(d) != w {
		t.Fatal("lost retained presentation")
	}
}

func TestWindowsClipboardConversionPreservesPixelsBeforeNativePublish(t *testing.T) {
	if _, err := windowsClipboardDIB([]byte("invalid")); err == nil {
		t.Fatal("malformed clipboard bytes accepted")
	}
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	im.SetNRGBA(0, 0, color.NRGBA{200, 40, 20, 255})
	im.SetNRGBA(2, 1, color.NRGBA{10, 90, 230, 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, im); err != nil {
		t.Fatal(err)
	}
	dib, err := windowsClipboardDIB(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := dibPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(roundtrip))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{0, 0}, {2, 1}} {
		r, g, b, a := decoded.At(p.X, p.Y).RGBA()
		wantR, wantG, wantB, wantA := im.At(p.X, p.Y).RGBA()
		if r != wantR || g != wantG || b != wantB || a != wantA {
			t.Fatal("clipboard colors or orientation changed")
		}
	}
}

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
	if c.claimPresentation("owned", 2) != nil || !d.ready {
		t.Fatal("disabled capture must record paint without revealing")
	}
	c.enabled = true
	d.ready = false
	c.driver.stopped.Store(true)
	if c.claimPresentation("owned", 2) != nil || d.ready {
		t.Fatal("shutdown capture accepted an acknowledgement")
	}
}

func TestWindowsCaptureLatePaintWhileDisabledRestoresSameUnknownRequest(t *testing.T) {
	w := &application.WebviewWindow{}
	saved := &windowsCaptureDraft{snapshot: screeninput.Snapshot{ID: "original"}, document: WindowsCaptureDocument{ID: "original", Outcome: "unknown", Presentation: 7}}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{}}
	d := c.restoreDraft(saved)
	d.window = w // The restored WebView exists but has not finished first paint.
	if c.retainedPresentation(d) != nil {
		t.Fatal("unpainted restoration revealed")
	}
	c.enabled = false
	if c.claimPresentation("original", 6) != nil || d.ready {
		t.Fatal("old presentation accepted while disabled")
	}
	if c.claimPresentation("original", 7) != nil || !d.ready || c.retainedPresentation(d) != nil {
		t.Fatal("late first paint must be retained without showing the window")
	}
	c.enabled = true
	restored := c.restoreDraft(&windowsCaptureDraft{document: WindowsCaptureDocument{ID: "original", Outcome: "unknown"}})
	if restored != d || c.retainedPresentation(restored) != w || restored.snapshot.ID != "original" || restored.document.ID != "original" || restored.document.Outcome != "unknown" || restored.document.Presentation != 7 {
		t.Fatal("reenable did not preserve original window, request and presentation")
	}
	if c.claimPresentation("original", 7) != nil {
		t.Fatal("duplicate first-paint receipt showed the window again")
	}
	delete(c.drafts, "original")
	d.ready = false
	if c.claimPresentation("original", 7) != nil || d.ready || c.retainedPresentation(d) != nil {
		t.Fatal("closed draft accepted a late receipt")
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

func TestWindowsCaptureDiscardsFinishedWindowsWithoutReusingOldFrames(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		outcome                                              string
		exporting, ready, enabled, stopped, occupied, retain bool
	}{
		{name: "finished selection", outcome: "draft", ready: true, enabled: true},
		{name: "accepted send", outcome: "accepted", ready: true, enabled: true},
		{name: "pending export", outcome: "draft", exporting: true, ready: true, enabled: true, retain: true},
		{name: "uncertain send", outcome: "unknown", ready: true, enabled: true, retain: true},
		{name: "failed first paint", outcome: "draft", enabled: true},
		{name: "windowed pin", outcome: "pinned", ready: true, enabled: true},
		{name: "disabled", outcome: "draft", ready: true},
		{name: "shutdown", outcome: "draft", ready: true, enabled: true, stopped: true},
		{name: "already warm", outcome: "draft", ready: true, enabled: true, occupied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &application.WebviewWindow{}
			d := &windowsCaptureDraft{window: w, slot: "slot", ready: tc.ready, exporting: tc.exporting, preview: []byte{1}, pixels: image.NewNRGBA(image.Rect(0, 0, 1, 1)), document: WindowsCaptureDocument{ID: "original", Outcome: tc.outcome, Image: "/capture-preview/original", Presentation: 2}}
			c := &windowsCapture{driver: &windowsDriver{}, enabled: tc.enabled, editorID: "original", drafts: map[string]*windowsCaptureDraft{"original": d}}
			c.driver.stopped.Store(tc.stopped)
			if tc.occupied {
				c.warmWindows = []windowsCaptureWarmWindow{{&application.WebviewWindow{}, "other"}}
			}
			current, retained := c.releasePresentation(d, w)
			if !current || retained != tc.retain || !d.dismissed || c.editorID != "" {
				t.Fatal("incorrect close lifecycle")
			}
			if tc.retain {
				if c.drafts["original"] != d || d.window != w || d.pixels == nil || len(d.preview) == 0 || d.document.Image == "" {
					t.Fatal("original request lost its pixels or presentation")
				}
			} else if c.drafts["original"] != nil || d.pixels != nil || d.preview != nil || d.document.Image != "" || d.window != nil {
				t.Fatal("finished image retained")
			}
			if (len(c.warmWindows) != 0) != tc.occupied || len(c.warmWindows) > 0 && c.warmWindows[0].window == w {
				t.Fatal("used compositor frame returned to warm slot")
			}
			if c.slotDocument("slot") != "" || c.claimPresentation("original", 2) != nil {
				t.Fatal("dismissed window revived")
			}
		})
	}
}

func TestWindowsCaptureClosingPreviousEditorPreservesReplacementPin(t *testing.T) {
	old, pin := &application.WebviewWindow{}, &application.WebviewWindow{}
	d := &windowsCaptureDraft{window: pin, slot: "pin-slot", pixels: image.NewNRGBA(image.Rect(0, 0, 4, 4)), document: WindowsCaptureDocument{ID: "original", Outcome: "pinned", Image: "flattened", Presentation: 2}}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{"original": d}}
	current, retained := c.releasePresentation(d, old)
	if current || retained || len(c.warmWindows) != 0 || c.drafts["original"] != d || d.window != pin || d.dismissed || d.pixels == nil || d.document.Image != "flattened" {
		t.Fatal("closing editor changed its replacement pin")
	}
	if c.slotDocument("screen-slot") != "" || c.slotDocument("pin-slot") != "original" || c.claimPresentation("original", 1) != nil || c.claimPresentation("original", 2) != pin {
		t.Fatal("old editor receipt affected pin presentation")
	}
}

func TestWindowsCaptureReservesDistinctFreshWindowsForEditorAndPin(t *testing.T) {
	editor, pin := &application.WebviewWindow{}, &application.WebviewWindow{}
	c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{}, warmWindows: []windowsCaptureWarmWindow{{editor, "editor"}, {pin, "pin"}}}
	if c.canPrewarmLocked() {
		t.Fatal("full pool admitted another renderer")
	}
	w, slot := c.takeWarmWindowLocked()
	if w != editor || slot != "editor" {
		t.Fatal("first command did not receive its own window")
	}
	c.editorID = "screen"
	c.drafts["screen"] = &windowsCaptureDraft{window: editor, ready: true, document: WindowsCaptureDocument{ID: "screen"}}
	if c.canPrewarmLocked() {
		t.Fatal("background window creation admitted during editor input")
	}
	w, slot = c.takeWarmWindowLocked()
	if w != pin || slot != "pin" || w == editor {
		t.Fatal("F3 did not receive a separate fresh window")
	}
	if w, slot = c.takeWarmWindowLocked(); w != nil || slot != "" {
		t.Fatal("consumed renderer was reused")
	}
	c.editorID = ""
	c.drafts["screen"].window = pin
	c.drafts["screen"].ready = false
	if c.canPrewarmLocked() {
		t.Fatal("replenishment competed with pin first paint")
	}
	c.drafts["screen"].ready = true
	if !c.canPrewarmLocked() {
		t.Fatal("ready pin did not allow replenishment")
	}
}

func TestWindowsCapturePrewarmAdmissionRespectsLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*windowsCapture)
	}{
		{"disabled", func(c *windowsCapture) { c.enabled = false }},
		{"stopped", func(c *windowsCapture) { c.driver.stopped.Store(true) }},
		{"acquiring", func(c *windowsCapture) { c.acquisition = &windowsCaptureAcquisition{screen: true} }},
		{"editor", func(c *windowsCapture) { c.editorID = "screen" }},
		{"document limit", func(c *windowsCapture) {
			for i := range 8 {
				c.drafts[string(rune('a'+i))] = &windowsCaptureDraft{ready: true}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &windowsCapture{driver: &windowsDriver{}, enabled: true, drafts: map[string]*windowsCaptureDraft{}}
			if !c.canPrewarmLocked() {
				t.Fatal("idle feature cannot prepare renderer")
			}
			tc.change(c)
			if c.canPrewarmLocked() {
				t.Fatal("inactive or busy feature admitted a renderer")
			}
		})
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

func TestWindowsCaptureDeferredCloseReleasesSettledExports(t *testing.T) {
	for _, outcome := range []string{"draft", "pinned", "unknown", "rejected", "accepted"} {
		t.Run(outcome, func(t *testing.T) {
			w := &application.WebviewWindow{}
			d := &windowsCaptureDraft{window: w, exporting: true, pixels: image.NewNRGBA(image.Rect(0, 0, 4, 4)), document: WindowsCaptureDocument{ID: "original", Outcome: outcome}}
			c := &windowsCapture{enabled: true, drafts: map[string]*windowsCaptureDraft{"original": d}}
			if current, retained := c.releasePresentation(d, w); !current || !retained {
				t.Fatal("closing during export did not defer pixel disposal")
			}
			closeWindow, _ := c.finishExport(d, false)
			if outcome == "unknown" {
				if closeWindow != nil || d.canCloseAfterOperation(false) || c.drafts["original"] != d || d.pixels == nil {
					t.Fatal("uncertain send lost its original receipt or pixels")
				}
				// A later authoritative rejection resolves the deferred close.
				d.document.Outcome = "rejected"
				if !d.canCloseAfterOperation(false) {
					t.Fatal("resolved dismissed send leaked its window")
				}
			} else if closeWindow != w {
				t.Fatal("cancelled or failed export leaked its dismissed window")
			}
			if current, retained := c.releasePresentation(d, w); !current || retained || len(c.drafts) != 0 || d.pixels != nil || d.window != nil {
				t.Fatal("settled export did not release its hidden native resources")
			}
		})
	}
}

func TestWindowsCaptureCancelledBeforeWindowAdmissionReleasesItsSlot(t *testing.T) {
	for _, outcome := range []string{"draft", "pinned", "unknown"} {
		d := &windowsCaptureDraft{dismissed: true, exporting: true, pixels: image.NewNRGBA(image.Rect(0, 0, 2, 2)), document: WindowsCaptureDocument{ID: "original", Outcome: outcome}}
		c := &windowsCapture{driver: &windowsDriver{}, enabled: true, editorID: "original", drafts: map[string]*windowsCaptureDraft{"original": d}}
		if c.releaseUnpresented(d) {
			t.Fatal("in-flight export lost its pixels")
		}
		c.finishExport(d, false)
		if outcome == "unknown" {
			if c.releaseUnpresented(d) || d.pixels == nil {
				t.Fatal("unpresented uncertain send was discarded")
			}
			d.document.Outcome = "rejected"
		}
		if !c.releaseUnpresented(d) || c.editorID != "" || len(c.drafts) != 0 || d.pixels != nil {
			t.Fatal("cancelled window admission leaked a document slot")
		}
		if c.releaseUnpresented(d) {
			t.Fatal("released the same draft twice")
		}
	}
}

func TestWindowsCaptureSelectionOnlyAndReceiptGate(t *testing.T) {
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
	export := WindowsCaptureExport{ID: id, Selection: screeninput.Rect{X: 12, Y: 12, Width: 24, Height: 24}, PNG: base64.StdEncoding.EncodeToString(b.Bytes()), Action: "send"}
	if e := s.ExportWindowsCapture(export); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(root, id, "context.jpg")); !os.IsNotExist(e) {
		t.Fatal("full-screen attachment exported", e)
	}
	data, e := os.ReadFile(filepath.Join(root, id, "selection.png"))
	if e != nil || !bytes.Equal(data, b.Bytes()) {
		t.Fatal("annotated selection changed", e)
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
	if d.pixels != pixels || d.document.Outcome != "draft" || d.document.Screen || d.snapshot.Source != "clipboard" {
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
