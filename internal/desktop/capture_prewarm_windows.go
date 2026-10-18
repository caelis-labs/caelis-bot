package desktop

import (
	"crypto/rand"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// One empty renderer, never a background screenshot. A slot is consumed only
// once; completed editors are destroyed, so pixels and draft state cannot leak
// into a later capture. Disabling the feature also destroys the unused slot.
func (c *windowsCapture) prewarm() {
	if c.driver.stopped.Load() {
		return
	}
	application.InvokeSync(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.enabled || c.driver.stopped.Load() || c.warmWindow != nil || len(c.drafts) >= 8 {
			return
		}
		slot := "capture-slot-" + rand.Text()
		w := c.driver.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: slot, URL: "/?surface=capture&slot=" + slot,
			Title: c.driver.service.text("native.capture.start", nil), Width: 880, Height: 640, Hidden: true, Frameless: true, AlwaysOnTop: true, DisableResize: true,
			Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true}})
		disableWindowsCaptureTransitions(uintptr(w.NativeWindow()))
		c.warmWindow, c.warmSlot = w, slot
		w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.warmWindow == w {
				c.warmWindow = nil
				c.warmSlot = ""
			}
		})
	})
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
		if d.slot == slot && d.window != nil {
			return d.document.ID
		}
	}
	return ""
}
