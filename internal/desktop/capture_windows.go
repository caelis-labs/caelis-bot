package desktop

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

type WindowsCaptureDocument struct {
	ID              string           `json:"id"`
	Image           string           `json:"image"`
	Width           int              `json:"width"`
	Height          int              `json:"height"`
	Window          screeninput.Rect `json:"window"`
	Screen          bool             `json:"screen"`
	Outcome         string           `json:"outcome"`
	ImageCapability string           `json:"imageCapability"`
	Message         string           `json:"message"`
	Presentation    uint64           `json:"presentation"`
	Diagnostics     bool             `json:"diagnostics"`
}
type WindowsCaptureExport struct {
	ID        string           `json:"id"`
	Selection screeninput.Rect `json:"selection"`
	PNG       string           `json:"png"`
	Note      string           `json:"note"`
	Action    string           `json:"action"`
}
type windowsCaptureDraft struct {
	started      time.Time
	slot         string
	document     WindowsCaptureDocument
	pixels       image.Image
	preview      []byte
	snapshot     screeninput.Snapshot
	window       *application.WebviewWindow
	exporting    bool
	ready        bool
	screenBounds image.Rectangle
	pinBounds    image.Rectangle
	dismissed    bool
	inputReports int
}
type windowsCaptureAcquisition struct {
	screen, cancelled bool
}
type windowsCapture struct {
	mu          sync.Mutex
	driver      *windowsDriver
	drafts      map[string]*windowsCaptureDraft
	imageState  string
	enabled     bool
	acquisition *windowsCaptureAcquisition
	editorID    string
	pinsHidden  bool
	saveImage   func(*application.WebviewWindow, []byte) error
	warmWindows []windowsCaptureWarmWindow
	prewarming  bool
}

func (d *windowsDriver) registerCaptureShortcut(s Shortcut, k int) error {
	return d.registerHotkey(k+3, s)
}
func (d *windowsDriver) captureEnabled(v bool) {
	d.capture.mu.Lock()
	d.capture.enabled = v
	d.capture.mu.Unlock()
	if !v {
		d.capture.closeWindows()
	} else {
		go d.capture.prewarm()
	}
}
func (d *windowsDriver) copyCaptureImage(b []byte) bool {
	dib, err := windowsClipboardDIB(b)
	if err != nil {
		return false
	}
	return application.InvokeSyncWithResult(func() bool { return windowsImageClipboard(uintptr(d.pet.NativeWindow()), b, dib) })
}
func (d *windowsDriver) captureCommand(k int) { go d.capture.command(k) }
func (d *windowsDriver) captureAvailability(v any) {
	b, _ := json.Marshal(v)
	var state struct{ State string }
	_ = json.Unmarshal(b, &state)
	d.capture.updateAvailability(state.State, func(w *application.WebviewWindow) {
		w.ExecJS("window.dispatchEvent(new Event('capture-receipt'))")
	})
}

// A renderer call synchronously enters Wails' UI thread. Never hold capture
// state across it: first paint, F3 and window close all read that same state.
func (c *windowsCapture) updateAvailability(state string, notify func(*application.WebviewWindow)) {
	c.mu.Lock()
	c.imageState = state
	var windows []*application.WebviewWindow
	for _, draft := range c.drafts {
		draft.document.ImageCapability = state
		if draft.window != nil {
			windows = append(windows, draft.window)
		}
	}
	c.mu.Unlock()
	for _, w := range windows {
		notify(w)
	}
}
func (d *windowsDriver) captureReceipt(v any) {
	b, _ := json.Marshal(v)
	var r struct{ ID, Outcome, Message string }
	_ = json.Unmarshal(b, &r)
	d.capture.mu.Lock()
	draft := d.capture.drafts[r.ID]
	var window *application.WebviewWindow
	closeWindow := false
	if draft != nil {
		draft.document.Outcome = r.Outcome
		draft.document.Message = r.Message
		window = draft.window
		closeWindow = draft.canCloseAfterOperation(false) || !d.capture.enabled && r.Outcome != "unknown"
	}
	d.capture.mu.Unlock()
	if window != nil {
		window.ExecJS("window.dispatchEvent(new Event('capture-receipt'))")
		if closeWindow {
			go d.service.CloseWindowsCapture(r.ID)
		}
	} else if draft != nil && d.capture.releaseUnpresented(draft) {
		go d.service.discardCapture(r.ID)
	}
}
func (d *windowsDriver) restoreCaptures(records []screeninput.Record) {
	go func() {
		for _, r := range records {
			b, e := os.ReadFile(filepath.Join(d.service.capture.root, r.Snapshot.ID, "selection.png"))
			if e != nil {
				continue
			}
			im, e := png.Decode(bytes.NewReader(b))
			if e != nil {
				continue
			}
			draft := &windowsCaptureDraft{pixels: im, snapshot: r.Snapshot, document: WindowsCaptureDocument{ID: r.Snapshot.ID, Width: im.Bounds().Dx(), Height: im.Bounds().Dy(), Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b), Outcome: r.Outcome}}
			draft = d.capture.restoreDraft(draft)
			d.capture.present(draft, image.Rectangle{})
		}
	}()
}

func (c *windowsCapture) restoreDraft(saved *windowsCaptureDraft) *windowsCaptureDraft {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current := c.drafts[saved.document.ID]; current != nil {
		current.dismissed = false
		return current
	}
	c.drafts[saved.document.ID] = saved
	return saved
}

func (c *windowsCapture) retainedPresentation(d *windowsCaptureDraft) *application.WebviewWindow {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled || c.driver.stopped.Load() || d.dismissed || c.drafts[d.document.ID] != d || !d.ready {
		return nil
	}
	return d.window
}

func (c *windowsCapture) closeWindows() {
	c.mu.Lock()
	if c.acquisition != nil {
		c.acquisition.cancelled = true
	}
	var close, hide []*application.WebviewWindow
	for _, warm := range c.warmWindows {
		close = append(close, warm.window)
	}
	c.warmWindows = nil
	for _, d := range c.drafts {
		if d.window != nil {
			if d.exporting || d.document.Outcome == "unknown" {
				hide = append(hide, d.window)
			} else {
				close = append(close, d.window)
			}
		}
	}
	c.mu.Unlock()
	if len(hide)+len(close) == 0 {
		return
	}
	application.InvokeSync(func() {
		for _, w := range hide {
			w.Hide()
		}
		for _, w := range close {
			w.Close()
		}
	})
}
func (c *windowsCapture) command(kind int) {
	started := time.Now()
	c.mu.Lock()
	if !c.enabled || c.driver.stopped.Load() {
		c.mu.Unlock()
		return
	}
	if kind == 2 {
		c.pinsHidden = !c.pinsHidden
		hidden := c.pinsHidden
		var pins []*application.WebviewWindow
		for _, d := range c.drafts {
			if d.document.Outcome == "pinned" && d.window != nil {
				pins = append(pins, d.window)
			}
		}
		c.mu.Unlock()
		application.InvokeSync(func() {
			for _, w := range pins {
				if hidden {
					w.Hide()
				} else {
					showWindowsWindow(w)
				}
			}
		})
		return
	}
	// F1 toggles the one editor, including while its pixels are still being
	// acquired. A late acquisition/paint acknowledgement cannot revive it.
	if kind == 0 {
		if handled, w := c.toggleScreenLocked(); handled {
			c.mu.Unlock()
			if w != nil {
				application.InvokeSync(func() { w.Hide(); w.Close() })
			}
			return
		}
	}
	if c.acquisition != nil {
		c.mu.Unlock()
		return
	}
	if len(c.drafts) >= 8 {
		c.mu.Unlock()
		c.driver.taskMessage("请先关闭已有截图 / Close an existing capture first")
		return
	}
	acquisition := &windowsCaptureAcquisition{screen: kind == 0}
	c.acquisition = acquisition
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.acquisition == acquisition {
			c.acquisition = nil
		}
		c.mu.Unlock()
	}()
	// Model discovery must not delay the user's local screenshot. Cached
	// availability gates Ask Bot; the asynchronous refresh updates it normally.
	go c.driver.service.refreshCapture()
	draft := &windowsCaptureDraft{snapshot: screeninput.Snapshot{Version: 1, ID: "capture-" + rand.Text(), CapturedAt: time.Now().Format(time.RFC3339Nano), Source: "screen"}}
	draft.started = started
	var bounds image.Rectangle
	if kind == 1 {
		_, b, e := readWindowsClipboard()
		if e != nil || len(b) == 0 {
			c.driver.taskMessage("剪贴板中没有可用图片 / No clipboard image")
			return
		}
		im, e := png.Decode(bytes.NewReader(b))
		if e != nil {
			return
		}
		draft.pixels = im
		// The clipboard already owns validated PNG bytes. Avoid compressing the
		// same image again before F3 can display it (especially a full screen).
		if len(b) <= 8<<20 {
			draft.document.Image = "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
		}
		draft.snapshot.Source = "clipboard"
	} else {
		// Identify the user's foreground window before presenting the overlay.
		hwnd, _, _ := winUser.NewProc("GetForegroundWindow").Call()
		var rect struct{ Left, Top, Right, Bottom int32 }
		winUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		var title [2049]uint16
		winUser.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&title[0])), 2049)
		draft.snapshot.WindowTitle = boundedWindowsCaptureText(windows.UTF16ToString(title[:]), 2048)
		var pid uint32
		winUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if handle, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); e == nil {
			var name [32768]uint16
			n := uint32(len(name))
			if windows.QueryFullProcessImageName(handle, 0, &name[0], &n) == nil {
				draft.snapshot.Application = boundedWindowsCaptureText(filepath.Base(windows.UTF16ToString(name[:n])), 512)
			}
			windows.CloseHandle(handle)
		}
		var point struct{ X, Y int32 }
		winUser.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
		packed := uintptr(uint32(point.X)) | uintptr(uint32(point.Y))<<32
		monitor, _, _ := winUser.NewProc("MonitorFromPoint").Call(packed, 2)
		var info struct {
			Size          uint32
			Monitor, Work struct{ Left, Top, Right, Bottom int32 }
			Flags         uint32
		}
		info.Size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := winUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return
		}
		bounds = image.Rect(int(info.Monitor.Left), int(info.Monitor.Top), int(info.Monitor.Right), int(info.Monitor.Bottom))
		draft.screenBounds = bounds
		r := image.Rect(int(rect.Left), int(rect.Top), int(rect.Right), int(rect.Bottom)).Intersect(bounds)
		draft.document.Window = screeninput.Rect{X: r.Min.X - bounds.Min.X, Y: r.Min.Y - bounds.Min.Y, Width: r.Dx(), Height: r.Dy()}
		// Temporarily hide only our presentation windows, preserving their state.
		var shown []*application.WebviewWindow
		application.InvokeSync(func() {
			for _, w := range []*application.WebviewWindow{c.driver.pet, c.driver.bubbleWindow, c.driver.taskWindow} {
				if w != nil && w.IsVisible() {
					shown = append(shown, w)
					w.Hide()
				}
			}
		})
		c.timing("hide", started)
		// Wait for this application's pending compositor updates, not a fixed
		// delay on every capture. No continuous screen observation is involved.
		flushWindowsCaptureFrame()
		grabStart := time.Now()
		im, e := windowsScreenPixels(bounds)
		c.timing("grab", grabStart)
		application.InvokeSync(func() {
			for _, w := range shown {
				showWindowsWindow(w)
			}
		})
		if e != nil {
			c.driver.taskMessage(c.driver.service.text("native.capture.failed", nil))
			return
		}
		draft.pixels = im
		previewStart := time.Now()
		draft.preview, e = windowsCapturePreview(im)
		c.timing("preview", previewStart)
		if e != nil {
			return
		}
	}
	b := draft.pixels.Bounds()
	draft.document.ID = draft.snapshot.ID
	draft.document.Width = b.Dx()
	draft.document.Height = b.Dy()
	if len(draft.preview) != 0 {
		draft.document.Image = "/capture-preview/" + draft.document.ID
	} else if draft.document.Image == "" {
		var encoded bytes.Buffer
		if e := encodeWindowsCapture(&encoded, draft.pixels); e != nil || encoded.Len() > 8<<20 {
			return
		}
		draft.document.Image = "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	}
	draft.document.Screen = draft.snapshot.Source == "screen"
	draft.document.Outcome = "draft"
	if kind == 1 {
		draft.document.Outcome = "pinned"
	}
	c.mu.Lock()
	if !c.admitAcquisitionLocked(acquisition, draft) {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.timing("acquired", started)
	c.present(draft, bounds)
	c.timing("window_created", started)
}

// Called with c.mu held. Native window calls always happen after unlocking.
func (c *windowsCapture) toggleScreenLocked() (bool, *application.WebviewWindow) {
	if c.acquisition != nil && c.acquisition.screen {
		c.acquisition.cancelled = true
		return true, nil
	}
	d := c.drafts[c.editorID]
	if d == nil || d.document.Outcome == "pinned" {
		return false, nil
	}
	d.dismissed = true
	if d.window == nil && !d.exporting && d.document.Outcome == "draft" {
		delete(c.drafts, c.editorID)
		c.editorID = ""
	}
	return true, d.window
}
func (c *windowsCapture) admitAcquisitionLocked(a *windowsCaptureAcquisition, d *windowsCaptureDraft) bool {
	if !c.enabled || c.driver.stopped.Load() || c.acquisition != a || a.cancelled {
		return false
	}
	d.document.ImageCapability = c.imageState
	d.document.Diagnostics = buildChannel == "development"
	c.drafts[d.document.ID] = d
	c.acquisition = nil
	if a.screen {
		c.editorID = d.document.ID
	}
	return true
}

func boundedWindowsCaptureText(value string, limit int) string {
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}
func (c *windowsCapture) present(d *windowsCaptureDraft, bounds image.Rectangle) {
	defer func() {
		if c.releaseUnpresented(d) {
			go c.driver.service.discardCapture(d.document.ID)
		}
	}()
	application.InvokeSync(func() {
		assigned := false
		c.mu.Lock()
		if c.driver.stopped.Load() || !c.enabled || d.dismissed || c.drafts[d.document.ID] != d {
			c.mu.Unlock()
			return
		}
		w := d.window
		created := w == nil
		if created {
			d.document.Presentation++
			d.ready = false
			pin := d.document.Outcome == "pinned"
			id, presentation, width, height, pinBounds := d.document.ID, d.document.Presentation, d.document.Width, d.document.Height, d.pinBounds
			if pin || !bounds.Empty() {
				w, d.slot = c.takeWarmWindowLocked()
				assigned = w != nil
			}
			if w == nil {
				d.slot = "capture-slot-" + rand.Text()
			}
			slot := d.slot
			c.mu.Unlock()
			// WebView creation can pump native messages. No capture/service lock
			// may span it, or a renderer read/close can deadlock the entire shell.
			if w == nil {
				w = c.driver.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: id, Title: c.driver.service.text("native.capture.start", nil), URL: "/?surface=capture&slot=" + slot, Width: 880, Height: 640, Frameless: pin || !bounds.Empty(), DisableResize: pin || !bounds.Empty(), AlwaysOnTop: true, Hidden: true, BackgroundType: application.BackgroundTypeTransparent, Windows: application.WindowsWindow{HiddenOnTaskbar: pin || !bounds.Empty(), DisableFramelessWindowDecorations: pin || !bounds.Empty()}})
				assigned = true
			}
			c.mu.Lock()
			valid := !c.driver.stopped.Load() && c.enabled && !d.dismissed && c.drafts[id] == d && d.document.Presentation == presentation && d.window == nil
			if valid {
				d.window = w
			}
			c.mu.Unlock()
			if !valid {
				w.Close()
				return
			}
			disableWindowsCaptureTransitions(uintptr(w.NativeWindow()))
			if assigned {
				const exStyle = ^uintptr(19)
				hwnd := uintptr(w.NativeWindow())
				style, _, _ := winUser.NewProc("GetWindowLongPtrW").Call(hwnd, exStyle)
				winUser.NewProc("SetWindowLongPtrW").Call(hwnd, exStyle, style&^uintptr(0x08000000))
			}
			if buildChannel == "development" {
				w.RegisterHook(events.Windows.WindowKeyDown, func(*application.WindowEvent) { c.reportInput(id, "native_key") })
			}
			if pin {
				positionWindowsPin(w, width, height, pinBounds)
				c.registerPinMenu(d, w)
			}
			// A failed renderer must not leave an invisible document consuming a
			// capture slot forever. The generation check excludes a replacement pin.
			time.AfterFunc(15*time.Second, func() {
				application.InvokeSync(func() {
					c.mu.Lock()
					expired := c.drafts[d.document.ID] == d && d.window == w && d.document.Presentation == presentation && !d.ready
					c.mu.Unlock()
					if expired && !c.driver.stopped.Load() {
						w.Close()
						c.driver.taskMessage(c.driver.service.text("native.capture.windowsImageFailed", nil))
					}
				})
			})
			w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
				current, retained := c.releasePresentation(d, w)
				if current && retained {
					e.Cancel()
					w.Hide()
				}
				if current && !retained {
					c.driver.app.ContextMenu.Remove(d.document.ID)
					go c.driver.service.discardCapture(d.document.ID)
				}
				if !current || !retained {
					go c.prewarm()
				}
			})
		} else {
			c.mu.Unlock()
		}
		if !bounds.Empty() {
			// Size the hidden HWND before decoding/drawing. Showing first exposes
			// the WebView's blank canvas across the entire display.
			winUser.NewProc("SetWindowPos").Call(uintptr(w.NativeWindow()), 0, uintptr(bounds.Min.X), uintptr(bounds.Min.Y), uintptr(bounds.Dx()), uintptr(bounds.Dy()), 0x0014)
		}
		if assigned {
			id, _ := json.Marshal(d.document.ID)
			w.ExecJS("window.dispatchEvent(new CustomEvent('capture-open',{detail:" + string(id) + "}))")
		}
		// An unknown submission retains its ready WebView while disabled. It
		// will not emit another first-paint receipt, so explicitly reveal it.
		if retained := c.retainedPresentation(d); retained != nil {
			showWindowsWindow(retained)
		}
	})
}

// Only the current presentation can reveal its window after its first image
// and selection have been painted. A late editor acknowledgement must not
// reveal a replacement pin, a closed document or a disabled capture feature.
func (s *Service) WindowsCaptureReady(id string, presentation uint64) {
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return
	}
	c := driver.capture
	application.InvokeSync(func() {
		if w := c.claimPresentation(id, presentation); w != nil {
			activateWindowsWindow(w)
			w.ExecJS("window.dispatchEvent(new Event('capture-focus'))")
			c.reportFocus(w, "shown")
			// Clipboard F3 consumes the same fresh slot as F1. Replenishment is
			// admitted only when no full-screen editor owns the keyboard.
			go c.prewarm()
			time.AfterFunc(1500*time.Millisecond, func() {
				application.InvokeSync(func() {
					if w.IsVisible() {
						c.reportFocus(w, "settled")
					}
				})
			})
		}
	})
}

// Development evidence contains only focus ownership, never screen text or keys.
func (c *windowsCapture) reportFocus(w *application.WebviewWindow, stage string) {
	if buildChannel != "development" || c.driver.diagnostic == nil {
		return
	}
	hwnd := uintptr(w.NativeWindow())
	front, _, _ := winUser.NewProc("GetForegroundWindow").Call()
	focus, _, _ := winUser.NewProc("GetFocus").Call()
	root, _, _ := winUser.NewProc("GetAncestor").Call(focus, 2)
	style, _, _ := winUser.NewProc("GetWindowLongPtrW").Call(hwnd, ^uintptr(19))
	c.driver.diagnostic("capture_focus", fmt.Sprintf("%s foreground=%t keyboard=%t child=%t noactivate=%t", stage, front == hwnd, root == hwnd, focus != hwnd, style&0x08000000 != 0))
}

// Bounded development diagnostics describe only the input route. Never record
// arbitrary keys, typed text, image content or document identities.
func (s *Service) WindowsCaptureInput(id, phase string) {
	s.mu.Lock()
	d, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if ok {
		d.capture.reportInput(id, phase)
	}
}
func (c *windowsCapture) reportInput(id, phase string) {
	if buildChannel != "development" || c.driver.diagnostic == nil {
		return
	}
	switch phase {
	case "native_key", "dom_focus", "dom_blur", "key_copy", "key_save", "key_send", "key_pin", "key_ime", "action_copy", "action_save", "action_send", "action_pin", "send_unavailable", "image_decoded":
	default:
		return
	}
	c.mu.Lock()
	d := c.drafts[id]
	if d == nil || d.inputReports >= 24 {
		c.mu.Unlock()
		return
	}
	d.inputReports++
	w := d.window
	started := d.started
	c.mu.Unlock()
	if phase == "image_decoded" {
		c.timing(phase, started)
		return
	}
	c.driver.diagnostic("capture_input", phase)
	if w != nil {
		application.InvokeSync(func() { c.reportFocus(w, phase) })
	}
}
func (c *windowsCapture) claimPresentation(id string, presentation uint64) *application.WebviewWindow {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.drafts[id]
	if c.driver.stopped.Load() || d == nil || d.dismissed || d.window == nil || d.ready || d.document.Presentation != presentation {
		return nil
	}
	// First paint is a fact about this presentation, even while disabled.
	// Retained unknown requests do not emit another acknowledgement on enable.
	d.ready = true
	c.timing("paint_ready", d.started)
	if !c.enabled {
		return nil
	}
	return d.window
}

func (c *windowsCapture) finishExport(d *windowsCaptureDraft, completed bool) (*application.WebviewWindow, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d.exporting = false
	// A receipt can arrive before the exporting call returns. Only acceptance
	// completes that operation; an unknown outcome must retain its original ID.
	if d.canCloseAfterOperation(completed) {
		return d.window, !c.enabled
	}
	return nil, !c.enabled
}

// A close during export is deferred until the operation settles. Cancellation
// or failure must then release the hidden window too, but an uncertain send
// retains its original receipt and pixels until reconciliation resolves it.
func (d *windowsCaptureDraft) canCloseAfterOperation(completed bool) bool {
	return completed || d.document.Outcome == "accepted" || d.dismissed && d.document.Outcome != "unknown"
}
func (s *Service) WindowsCaptureDocument(id string) (WindowsCaptureDocument, error) {
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return WindowsCaptureDocument{}, errors.New("capture unavailable")
	}
	driver.capture.mu.Lock()
	defer driver.capture.mu.Unlock()
	d := driver.capture.drafts[id]
	if d == nil {
		return WindowsCaptureDocument{}, errors.New("capture document unavailable")
	}
	return d.document, nil
}
func (s *Service) RefreshWindowsCapture(id string) {
	if d, e := s.WindowsCaptureDocument(id); e == nil && d.Outcome != "pinned" {
		s.refreshCapture()
	}
}
func (s *Service) CloseWindowsCapture(id string) error {
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return errors.New("capture unavailable")
	}
	driver.capture.mu.Lock()
	d := driver.capture.drafts[id]
	var window *application.WebviewWindow
	if d != nil {
		window = d.window
		if window == nil {
			d.dismissed = true
		}
	}
	driver.capture.mu.Unlock()
	if d == nil {
		return errors.New("capture unavailable")
	}
	if window != nil {
		window.Close()
	} else if driver.capture.releaseUnpresented(d) {
		go s.discardCapture(id)
	}
	return nil
}
func (s *Service) ExportWindowsCapture(v WindowsCaptureExport) error {
	started := time.Now()
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	enabled := s.capture.preferences.Enabled
	s.mu.Unlock()
	if !ok || !enabled {
		return errors.New("capture unavailable")
	}
	c := driver.capture
	c.mu.Lock()
	d := c.drafts[v.ID]
	if d == nil || d.dismissed || d.exporting || d.document.Outcome == "unknown" || d.document.Outcome == "accepted" {
		c.mu.Unlock()
		return errors.New("capture has an unresolved or accepted submission")
	}
	d.exporting = true
	pinned := d.document.Outcome == "pinned"
	c.mu.Unlock()
	closeCompleted := false
	defer func() {
		window, disabled := c.finishExport(d, closeCompleted)
		// Clear exporting before Close: the closing hook otherwise retains a
		// completed document as an unknown in-flight operation.
		if window != nil {
			window.Close()
		}
		if c.releaseUnpresented(d) {
			go s.discardCapture(d.document.ID)
		}
		if disabled {
			go c.closeWindows()
		}
	}()
	if pinned {
		if v.Action != "copy" && v.Action != "save" {
			return errors.New("a pin can only be copied, saved or re-edited")
		}
		data, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(d.document.Image, "data:image/png;base64,"))
		if e != nil {
			return e
		}
		if v.Action == "save" {
			err := c.save(d.window, data)
			if errors.Is(err, errWindowsCaptureSaveCancelled) {
				return nil
			}
			return err
		}
		if !driver.copyCaptureImage(data) {
			return errors.New("clipboard unavailable")
		}
		return nil
	}
	r := image.Rect(v.Selection.X, v.Selection.Y, v.Selection.X+v.Selection.Width, v.Selection.Y+v.Selection.Height)
	if r.Empty() || !r.In(d.pixels.Bounds()) || len(v.Note) > 4096 || len(v.PNG) > 12<<20 {
		return errors.New("invalid capture selection")
	}
	data, e := base64.StdEncoding.DecodeString(v.PNG)
	if e != nil || len(data) > 8<<20 {
		return errors.New("invalid capture image")
	}
	config, e := png.DecodeConfig(bytes.NewReader(data))
	if e != nil || config.Width != r.Dx() || config.Height != r.Dy() {
		return errors.New("capture image dimensions changed")
	}
	selection, e := png.Decode(bytes.NewReader(data))
	if e != nil {
		return e
	}
	if v.Action == "copy" {
		if !driver.copyCaptureImage(data) {
			return errors.New("clipboard unavailable")
		}
		closeCompleted = true
		return nil
	}
	if v.Action == "save" {
		err := c.save(d.window, data)
		if errors.Is(err, errWindowsCaptureSaveCancelled) {
			return nil
		}
		closeCompleted = err == nil
		return err
	}
	if v.Action != "send" && v.Action != "pin" {
		return errors.New("invalid capture action")
	}
	dir := filepath.Join(s.capture.root, v.ID)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e = localstate.ProtectDirectory(dir); e != nil {
		return e
	}
	snapshot := d.snapshot
	snapshot.Note = v.Note
	if e = os.WriteFile(filepath.Join(dir, "selection.png"), data, 0600); e != nil {
		return e
	}
	if e = localstate.Write(filepath.Join(dir, "capture.json"), snapshot); e != nil {
		return e
	}
	if v.Action == "send" {
		c.mu.Lock()
		d.document.Outcome = "unknown"
		c.mu.Unlock()
		go s.sendCapture(v.ID)
		return nil
	}
	c.mu.Lock()
	if d.dismissed {
		c.mu.Unlock()
		return nil
	}
	d.pinBounds = windowsPinSelectionBounds(r, d.screenBounds, d.pinBounds, d.document.Width, d.document.Height)
	// Measure pin export/presentation, excluding time spent choosing a region.
	d.started = started
	d.document.Outcome = "pinned"
	if c.editorID == v.ID {
		c.editorID = ""
	}
	d.document.Image = "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	d.document.Width = r.Dx()
	d.document.Height = r.Dy()
	d.document.Screen = false
	d.document.Window = screeninput.Rect{}
	d.pixels = selection
	d.preview = nil
	oldWindow := d.window
	d.window = nil
	c.mu.Unlock()
	c.present(d, image.Rectangle{})
	if oldWindow != nil {
		oldWindow.Close()
	}
	return nil
}

var errWindowsCaptureSaveCancelled = errors.New("capture save cancelled")

// The native picker owns the destination; renderer input never supplies a path.
func (c *windowsCapture) save(window *application.WebviewWindow, data []byte) error {
	if c.saveImage != nil {
		return c.saveImage(window, data)
	}
	path, err := c.driver.app.Dialog.SaveFile().AttachToWindow(window).
		SetFilename("Caelis-capture.png").AddFilter("PNG", "*.png").PromptForSingleSelection()
	// Wails beta.28 returns its internal cfd.ErrorCancelled directly. It is not
	// exported for errors.Is; match only that exact pinned dependency sentinel.
	if err != nil && err.Error() == "cancelled by user" {
		return errWindowsCaptureSaveCancelled
	}
	if err != nil {
		return err
	}
	if path == "" {
		return errWindowsCaptureSaveCancelled
	}
	return saveWindowsCapturePNG(path, data)
}

func saveWindowsCapturePNG(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".caelis-capture-*.png")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	source, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, destination, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// Re-edit the already flattened pin. Original unredacted pixels are not restored.
func (s *Service) EditWindowsCapture(id string) error {
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return errors.New("capture unavailable")
	}
	c := driver.capture
	if c == nil {
		return errors.New("capture unavailable")
	}
	d, oldWindow, err := c.editPin(id)
	if err != nil {
		return err
	}
	if oldWindow != nil {
		application.InvokeSync(func() {
			var r struct{ Left, Top, Right, Bottom int32 }
			if ok, _, _ := winUser.NewProc("GetWindowRect").Call(uintptr(oldWindow.NativeWindow()), uintptr(unsafe.Pointer(&r))); ok != 0 {
				c.mu.Lock()
				d.pinBounds = image.Rect(int(r.Left), int(r.Top), int(r.Right), int(r.Bottom))
				c.mu.Unlock()
			}
		})
	}
	c.present(d, image.Rectangle{})
	if oldWindow != nil {
		oldWindow.Close()
	}
	return nil
}

func (c *windowsCapture) editPin(id string) (*windowsCaptureDraft, *application.WebviewWindow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.drafts[id]
	if !c.enabled || d == nil || d.exporting || d.document.Outcome != "pinned" {
		return nil, nil, errors.New("capture pin unavailable")
	}
	d.document.Outcome = "draft"
	d.dismissed = false
	d.screenBounds = image.Rectangle{}
	c.editorID = id
	d.document.Message = ""
	d.snapshot = screeninput.Snapshot{Version: 1, ID: id, CapturedAt: time.Now().Format(time.RFC3339Nano), Source: "clipboard"}
	oldWindow := d.window
	d.window = nil
	return d, oldWindow, nil
}
