package desktop

import (
	"crypto/rand"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type windowsCaptureWarmWindow struct {
	window *application.WebviewWindow
	slot   string
}

// Reserve an editor and its subsequent pin. No screenshot pixels are acquired
// until the user's command. Both slots always contain fresh empty renderers.
const windowsCaptureWarmSlots = 2

// A used HWND is destroyed: hidden WebViews can retain a compositor frame even after React
// unmounts, flashing an old selection when shown again. Prewarm its replacement
// only after the editor closes or a clipboard pin is ready, without activation.
func (c *windowsCapture) prewarm() {
	if c.driver.stopped.Load() {
		return
	}
	application.InvokeSync(func() {
		c.mu.Lock()
		if c.prewarming || !c.canPrewarmLocked() {
			c.mu.Unlock()
			return
		}
		c.prewarming = true
		c.mu.Unlock()
		defer func() { c.mu.Lock(); c.prewarming = false; c.mu.Unlock() }()
		for {
			c.mu.Lock()
			admitted := c.canPrewarmLocked()
			c.mu.Unlock()
			if !admitted {
				return
			}
			slot := "capture-slot-" + rand.Text()
			w := c.driver.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: slot, URL: "/?surface=capture&slot=" + slot,
				Title: c.driver.service.text("native.capture.start", nil), Width: 880, Height: 640, Hidden: true, Frameless: true, AlwaysOnTop: true, DisableResize: true, BackgroundType: application.BackgroundTypeTransparent,
				// Wails' frameless setup calls SetWindowPos(SWP_FRAMECHANGED)
				// without SWP_NOACTIVATE even for Hidden windows. Keep this empty
				// renderer non-activating until it is assigned to a user capture.
				Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, ExStyle: 0x08210080}})
			disableWindowsCaptureTransitions(uintptr(w.NativeWindow()))
			c.mu.Lock()
			if !c.canPrewarmLocked() {
				c.mu.Unlock()
				w.Close()
				return
			}
			c.warmWindows = append(c.warmWindows, windowsCaptureWarmWindow{w, slot})
			c.mu.Unlock()
			w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
				c.mu.Lock()
				defer c.mu.Unlock()
				for i, warm := range c.warmWindows {
					if warm.window == w {
						c.warmWindows = append(c.warmWindows[:i], c.warmWindows[i+1:]...)
						break
					}
				}
			})
		}
	})
}

// State-only helpers run under c.mu. Never wait for WebView creation while
// holding this lock, and never replenish the pool during editor interaction.
func (c *windowsCapture) canPrewarmLocked() bool {
	if !c.enabled || c.driver.stopped.Load() || len(c.warmWindows) >= windowsCaptureWarmSlots || c.acquisition != nil || c.editorID != "" || len(c.drafts) >= 8 {
		return false
	}
	for _, d := range c.drafts {
		if !d.ready {
			return false
		}
	}
	return true
}
func (c *windowsCapture) takeWarmWindowLocked() (*application.WebviewWindow, string) {
	if len(c.warmWindows) == 0 {
		return nil, ""
	}
	warm := c.warmWindows[0]
	c.warmWindows[0] = windowsCaptureWarmWindow{}
	c.warmWindows = c.warmWindows[1:]
	return warm.window, warm.slot
}

// The initial read closes the race where assignment precedes listener setup.
// It resolves only this native-owned slot, without exposing paths or images.
func (s *Service) WindowsCaptureSlot(slot string) string {
	s.mu.Lock()
	driver, ok := s.native.(*windowsDriver)
	s.mu.Unlock()
	if !ok {
		return ""
	}
	return driver.capture.slotDocument(slot)
}
func (c *windowsCapture) slotDocument(slot string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot == "" || !c.enabled || c.driver.stopped.Load() {
		return ""
	}
	for _, d := range c.drafts {
		if d.slot == slot && d.window != nil && !d.dismissed {
			return d.document.ID
		}
	}
	return ""
}

// Closing presentation is independent of the request receipt. An exporting or
// uncertain document owns its original window and pixels until reconciliation.
// Closing a replaced editor cannot modify its new pin. Never return a used
// window to the warm slot, even after its renderer state has been cleared.
func (c *windowsCapture) releasePresentation(d *windowsCaptureDraft, w *application.WebviewWindow) (current, retained bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current = d.window == w
	if current {
		d.dismissed = true
		if c.editorID == d.document.ID {
			c.editorID = ""
		}
	}
	retained = d.exporting || d.document.Outcome == "unknown"
	if current && !retained {
		delete(c.drafts, d.document.ID)
		d.pixels, d.preview, d.document.Image = nil, nil, ""
		d.window = nil
	}
	return
}

// Disabling/closing can race a new window's creation or replacement. If no
// HWND was admitted, there will be no closing hook to release that draft.
func (c *windowsCapture) releaseUnpresented(d *windowsCaptureDraft) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.drafts[d.document.ID] != d || d.window != nil || d.exporting || d.document.Outcome == "unknown" ||
		(!d.dismissed && c.enabled && !c.driver.stopped.Load()) {
		return false
	}
	delete(c.drafts, d.document.ID)
	if c.editorID == d.document.ID {
		c.editorID = ""
	}
	d.pixels, d.preview, d.document.Image = nil, nil, ""
	return true
}
