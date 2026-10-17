package desktop

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"math"
	"strings"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var captureDWM = windows.NewLazySystemDLL("dwmapi.dll")

func flushWindowsCaptureFrame() { captureDWM.NewProc("DwmFlush").Call() }
func disableWindowsCaptureTransitions(hwnd uintptr) {
	var yes int32 = 1
	captureDWM.NewProc("DwmSetWindowAttribute").Call(hwnd, 3, uintptr(unsafe.Pointer(&yes)), 4) // DWMWA_TRANSITIONS_FORCEDISABLED
}
func encodeWindowsCapture(out *bytes.Buffer, im image.Image) error {
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(out, im); err != nil {
		return err
	}
	// Retain the existing byte budget; a compressible large image must not be
	// rejected merely because the fast encoder produced a bigger preview.
	if out.Len() > 8<<20 {
		out.Reset()
		return png.Encode(out, im)
	}
	return nil
}

func windowsPinWorkArea(hwnd uintptr) image.Rectangle {
	monitor, _, _ := winUser.NewProc("MonitorFromWindow").Call(hwnd, 2)
	var info struct {
		Size          uint32
		Monitor, Work struct{ Left, Top, Right, Bottom int32 }
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := winUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return image.Rect(0, 0, 1280, 720)
	}
	return image.Rect(int(info.Work.Left), int(info.Work.Top), int(info.Work.Right), int(info.Work.Bottom))
}

// Physical pixels throughout: preserve aspect ratio and position on mixed-DPI
// desktops, including monitors whose work area has a negative origin.
func fitWindowsPin(width, height int, desired float64, work image.Rectangle, x, y int) image.Rectangle {
	if width < 1 || height < 1 || work.Empty() {
		return image.Rectangle{}
	}
	maximum := math.Min(float64(work.Dx())/float64(width), float64(work.Dy())/float64(height))
	minimum := math.Min(maximum, 24/float64(min(width, height)))
	scale := math.Min(maximum, math.Max(minimum, desired))
	w, h := max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
	x = max(work.Min.X, min(x, work.Max.X-w))
	y = max(work.Min.Y, min(y, work.Max.Y-h))
	return image.Rect(x, y, x+w, y+h)
}
func setWindowsPinBounds(w *application.WebviewWindow, r image.Rectangle) {
	winUser.NewProc("SetWindowPos").Call(uintptr(w.NativeWindow()), 0, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), 0x0014)
}
func positionWindowsPin(w *application.WebviewWindow, width, height int) {
	area := windowsPinWorkArea(uintptr(w.NativeWindow()))
	r := fitWindowsPin(width, height, 1, area, area.Min.X, area.Min.Y)
	r = r.Add(image.Pt((area.Dx()-r.Dx())/2, (area.Dy()-r.Dy())/2))
	setWindowsPinBounds(w, r)
}

func (s *Service) ResizeWindowsPin(id string, factor float64) {
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor < 0.5 || factor > 2 {
		return
	}
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return
	}
	application.InvokeSync(func() {
		c := driver.capture
		c.mu.Lock()
		defer c.mu.Unlock()
		d := c.drafts[id]
		if !c.enabled || d == nil || d.window == nil || d.document.Outcome != "pinned" || d.exporting {
			return
		}
		hwnd := uintptr(d.window.NativeWindow())
		var r struct{ Left, Top, Right, Bottom int32 }
		if ok, _, _ := winUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
			return
		}
		scale := float64(r.Right-r.Left) / float64(d.document.Width) * factor
		setWindowsPinBounds(d.window, fitWindowsPin(d.document.Width, d.document.Height, scale, windowsPinWorkArea(hwnd), int(r.Left), int(r.Top)))
	})
}

func (c *windowsCapture) registerPinMenu(d *windowsCaptureDraft, w *application.WebviewWindow) {
	m := c.driver.app.ContextMenu.New()
	for _, item := range []struct{ key, action string }{{"copy", "copy"}, {"save", "save"}, {"edit", "edit"}, {"closePin", "close"}} {
		action, _ := json.Marshal(item.action)
		label := strings.ReplaceAll(c.driver.service.text("native.capture."+item.key, nil), "⌘", "Ctrl+")
		m.Add(label).OnClick(func(*application.Context) {
			w.ExecJS("window.dispatchEvent(new CustomEvent('capture-pin-action',{detail:" + string(action) + "}))")
		})
	}
	c.driver.app.ContextMenu.Add(d.document.ID, m)
}
