//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework UserNotifications -framework Carbon
#include "native_darwin.h"
#include "material_darwin.h"
#include <stdlib.h>
*/
import "C"
import (
	_ "embed"
	"encoding/json"
	"errors"
	"runtime/cgo"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/status-icon.png
var statusIcon []byte

type macDriver struct {
	events *nativeEventQueue

	pointer unsafe.Pointer
	handle  cgo.Handle
}

func newMacDriver(pet, panel, bubble, history, prop *application.WebviewWindow, s *Service, quit func()) *macDriver {
	d := &macDriver{}
	d.events = newNativeEventQueue(func(e nativeEvent) {
		switch e.kind {
		case 1:
			s.moved(e.x, e.y)
		case 2:
			s.OpenHistory()
		case 3:
			s.recoverDisplay()
		case 5:
			logError(s.SetVisible(e.x != 0))
		case 6:
			logError(s.resized(e.x, e.y, e.scale))
		case 7:
			quit()
		case 8:
			s.Activate()
		case 9:
			s.OpenSettings()
		case 10:
			s.OpenUpdates()
		case 11:
			s.ToggleCenteredPanel()
		}
	})
	d.handle = cgo.NewHandle(d.events)
	application.InvokeSync(func() {
		d.pointer = C.bot_create(pet.NativeWindow(), panel.NativeWindow(), bubble.NativeWindow(), history.NativeWindow(), prop.NativeWindow(), C.uintptr_t(d.handle), (*C.uchar)(unsafe.Pointer(&statusIcon[0])), C.int(len(statusIcon)))
	})
	return d
}
func (d *macDriver) screens() []Rect {
	return application.InvokeSyncWithResult(func() []Rect {
		var rects [32]C.BotRect
		n := int(C.bot_screens(&rects[0], 32))
		result := make([]Rect, n)
		for i := range n {
			r := rects[i]
			result[i] = Rect{float64(r.x), float64(r.y), float64(r.width), float64(r.height)}
		}
		return result
	})
}
func (d *macDriver) apply(p Placement) {
	application.InvokeSync(func() {
		v := 0
		if p.Visible {
			v = 1
		}
		C.bot_apply(d.pointer, C.double(p.X), C.double(p.Y), C.double(p.Scale), C.int(v))
	})
}
func (d *macDriver) panelHeight(height int) {
	application.InvokeSync(func() { C.bot_panel_height(d.pointer, C.int(height)) })
}
func (d *macDriver) panelMenu(height, activation int) {
	application.InvokeSync(func() { C.bot_panel_menu(d.pointer, C.int(height), C.int(activation)) })
}
func (d *macDriver) togglePanel() {
	application.InvokeSync(func() { C.bot_toggle_panel(d.pointer) })
}
func (d *macDriver) approval() {
	application.InvokeSync(func() { C.bot_expand_bubble(d.pointer, 1) })
}
func (d *macDriver) bubble(visible bool) {
	application.InvokeSync(func() {
		v := 0
		if visible {
			v = 1
		}
		C.bot_bubble(d.pointer, C.int(v))
	})
}
func (d *macDriver) panel(visible bool) {
	application.InvokeSync(func() {
		v := 0
		if visible {
			v = 1
		}
		C.bot_panel(d.pointer, C.int(v))
	})
}
func (d *macDriver) mask(b []byte) {
	application.InvokeSync(func() { C.bot_mask(d.pointer, (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b))) })
}
func (d *macDriver) stop() {
	d.events.stop()
	application.InvokeSync(func() { C.bot_destroy(d.pointer) })
	d.handle.Delete()
}

func (d *macDriver) notify(id, title, body string, reminder bool) {
	a, b, c := C.CString(id), C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(b))
	defer C.free(unsafe.Pointer(c))
	application.InvokeSync(func() {
		flag := 0
		if reminder {
			flag = 1
		}
		C.bot_notify(d.pointer, a, b, c, C.int(flag))
	})
}
func (d *macDriver) notificationStatus() string {
	return application.InvokeSyncWithResult(func() string {
		switch int(C.bot_notification_status(d.pointer)) {
		case 0:
			return "notDetermined"
		case 1:
			return "denied"
		case 2, 3:
			return "authorized"
		default:
			return "unavailable"
		}
	})
}
func (d *macDriver) configureNotifications() {
	application.InvokeSync(func() { C.bot_configure_notifications(d.pointer) })
}

//export desktopEvent
func desktopEvent(handle C.uintptr_t, kind C.int, x, y, scale C.double) {
	// Never wait for the service lock from AppKit: a service call may be waiting
	// on the main thread. A single consumer preserves resize -> hide/quit order.
	cgo.Handle(handle).Value().(*nativeEventQueue).push(nativeEvent{int(kind), float64(x), float64(y), float64(scale)})
}

func (d *macDriver) expandBubble(expanded bool) {
	application.InvokeSync(func() {
		v := 0
		if expanded {
			v = 1
		}
		C.bot_expand_bubble(d.pointer, C.int(v))
	})
}
func (d *macDriver) bubbleHeight(height int) {
	application.InvokeSync(func() { C.bot_bubble_height(d.pointer, C.int(height)) })
}

func (d *macDriver) gesture(action string) {
	value := C.CString(action)
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.bot_gesture(d.pointer, value) })
}

func trashNativePath(path string) error {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	ok := application.InvokeSyncWithResult(func() bool { return C.bot_trash_path(value) != 0 })
	if !ok {
		return errors.New("无法移到废纸篓")
	}
	return nil
}

// Install a decorative native sidebar behind WebKit without replacing its
// content view or responder/accessibility hierarchy.
func styleMacSettings(window *application.WebviewWindow) {
	application.InvokeSync(func() { C.bot_style_settings(window.NativeWindow()) })
}

func syncMacMaterials() {
	application.InvokeSync(func() { C.bot_sync_material_pages() })
}

func (d *macDriver) activity(value string) {
	text := C.CString(value)
	defer C.free(unsafe.Pointer(text))
	application.InvokeSync(func() { C.bot_activity(d.pointer, text) })
}

func (d *macDriver) desktopContext() json.RawMessage {
	return application.InvokeSyncWithResult(func() json.RawMessage {
		p := C.bot_context(d.pointer)
		defer C.free(unsafe.Pointer(p))
		return json.RawMessage(C.GoString(p))
	})
}
func (d *macDriver) planeReady(ready bool) {
	application.InvokeSync(func() {
		v := 0
		if ready {
			v = 1
		}
		C.bot_plane_ready(d.pointer, C.int(v))
	})
}
func (d *macDriver) launchPlane(id string, x, y float64) bool {
	text := C.CString(id)
	defer C.free(unsafe.Pointer(text))
	return application.InvokeSyncWithResult(func() bool { return C.bot_launch_plane(d.pointer, text, C.double(x), C.double(y)) != 0 })
}
func (d *macDriver) finishPlane(id string, completed bool) {
	text := C.CString(id)
	defer C.free(unsafe.Pointer(text))
	application.InvokeSync(func() {
		v := 0
		if completed {
			v = 1
		}
		C.bot_finish_plane(d.pointer, text, C.int(v))
	})
}

func (d *macDriver) registerShortcut(v Shortcut) error {
	key := C.CString(v.Key)
	defer C.free(unsafe.Pointer(key))
	flags := 0
	if v.Control {
		flags |= 1
	}
	if v.Alt {
		flags |= 2
	}
	if v.Shift {
		flags |= 4
	}
	if v.Meta {
		flags |= 8
	}
	enabled := 0
	if v.Enabled {
		enabled = 1
	}
	status := application.InvokeSyncWithResult(func() int { return int(C.bot_shortcut(d.pointer, key, C.int(flags), C.int(enabled))) })
	if status != 0 {
		return errors.New("该快捷键已被系统或其他应用占用，请选择其他组合；原快捷键保持不变")
	}
	return nil
}
func (d *macDriver) centeredPanel() {
	application.InvokeSync(func() { C.bot_centered_panel(d.pointer) })
}
func (d *macDriver) panelReady(id int) {
	application.InvokeSync(func() { C.bot_panel_ready(d.pointer, C.int(id)) })
}

var (
	_ driver             = (*macDriver)(nil)
	_ shortcutDriver     = (*macDriver)(nil)
	_ notificationDriver = (*macDriver)(nil)
	_ panelMenuDriver    = (*macDriver)(nil)
	_ bubbleDriver       = (*macDriver)(nil)
	_ activityDriver     = (*macDriver)(nil)
	_ gestureDriver      = (*macDriver)(nil)
	_ contextDriver      = (*macDriver)(nil)
	_ propDriver         = (*macDriver)(nil)
)
