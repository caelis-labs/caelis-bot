package desktop

import (
	"encoding/json"
	"os"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UI-thread-owned input facts; moving back to the starting position is still a
// drag. Native cancellation must never become a delayed single click.
type windowsPetInteraction struct {
	Pressing         bool `json:"pressing"`
	Dragging         bool `json:"dragging"`
	Input            bool `json:"input"`
	Menu             bool `json:"menu"`
	moved, cancelled bool
}

func (s *windowsPetInteraction) begin() {
	s.Pressing, s.Dragging, s.moved, s.cancelled = true, false, false, false
}
func (s *windowsPetInteraction) move()       { s.Dragging, s.moved = true, true }
func (s *windowsPetInteraction) end()        { s.Pressing, s.Dragging = false, false }
func (s *windowsPetInteraction) cancel()     { s.end(); s.cancelled = true }
func (s *windowsPetInteraction) click() bool { return !s.moved && !s.cancelled }

const windowsPetContextTimer = 0xCB02

// DesktopContext is read while Service.mu may be held. Return an immutable
// cached snapshot, never wait for the UI thread from that lock.
func (d *windowsDriver) desktopContext() json.RawMessage {
	if v := d.contextSnapshot.Load(); v != nil {
		return append(json.RawMessage(nil), v.(json.RawMessage)...)
	}
	return json.RawMessage("null")
}

func (d *windowsDriver) publishContext() {
	if d.stopped.Load() || !d.placement.Visible {
		return
	}
	hwnd := uintptr(d.pet.NativeWindow())
	var mouse struct{ X, Y int32 }
	if ok, _, _ := winUser.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&mouse))); ok == 0 {
		return
	}
	p := d.app.Screen.PhysicalToDipPoint(application.Point{X: int(mouse.X), Y: int(mouse.Y)})
	x, y := d.pet.Position()
	w, h := d.pet.Size()
	screen := d.app.Screen.ScreenNearestDipPoint(application.Point{X: x, Y: y})
	if screen == nil {
		return
	}
	height := d.primaryHeight()
	rect := func(r application.Rect) map[string]float64 {
		return map[string]float64{"x": float64(r.X), "y": height - float64(r.Y+r.Height), "width": float64(r.Width), "height": float64(r.Height)}
	}
	front, _, _ := winUser.NewProc("GetForegroundWindow").Call()
	var pid uint32
	winUser.NewProc("GetWindowThreadProcessId").Call(front, uintptr(unsafe.Pointer(&pid)))
	d.interaction.Input = pid == uint32(os.Getpid()) && front != hwnd
	packed := uintptr(uint32(mouse.X)) | uintptr(uint32(mouse.Y))<<32
	under, _, _ := winUser.NewProc("WindowFromPoint").Call(packed)
	// WebView2 may expose a child HWND at the pointer. Resolve only the parent
	// chain (not owned windows), so the pet's content counts as hover while its
	// separate bubble, task dock and an overlaid external window do not.
	underRoot, _, _ := winUser.NewProc("GetAncestor").Call(under, 2) // GA_ROOT
	value := map[string]any{
		"desktop": map[string]any{"id": screen.ID, "frame": rect(screen.Bounds), "workArea": rect(screen.WorkArea), "scale": screen.ScaleFactor},
		// Rich external desktop/active-window semantics remain a separate feature.
		"dock":         map[string]any{"edge": "unknown", "confidence": "unknown", "source": "unavailable", "frame": nil},
		"activeWindow": map[string]any{"application": "", "id": nil, "observedAtUptime": 0, "frame": nil, "source": "unavailable"},
		"actor":        rect(application.Rect{X: x, Y: y, Width: w, Height: h}),
		"pointer":      map[string]any{"x": p.X, "y": height - float64(p.Y), "hovering": underRoot == hwnd && !d.interaction.Dragging},
		"interaction":  d.interaction,
	}
	state, _ := json.Marshal(value)
	if string(state) == d.lastContext {
		return
	}
	d.lastContext = string(state)
	d.contextRevision++
	value["revision"] = d.contextRevision
	value["sampledAtUptime"] = time.Since(d.contextStarted).Seconds()
	data, _ := json.Marshal(value)
	d.contextSnapshot.Store(json.RawMessage(data))
	d.pet.ExecJS("window.dispatchEvent(new CustomEvent('pet-context',{detail:" + string(data) + "}))")
}

func (d *windowsDriver) beginPetMove() {
	first := !d.interaction.Dragging
	d.interaction.move()
	if first {
		d.bubbleWindow.ExecJS("window.dispatchEvent(new Event('bubble-hidden'))")
		d.bubbleWindow.Hide()
		if d.taskWindow != nil {
			d.taskWindow.Hide()
		}
	}
	d.publishContext()
}

func (d *windowsDriver) endPetMove() {
	d.interaction.end()
	d.publishContext()
	if d.stopped.Load() {
		return
	}
	d.layout()
	d.refreshTaskDock()
	if d.placement.Visible && d.bubbleWanted {
		showWindowsWindow(d.bubbleWindow)
	}
}

var _ contextDriver = (*windowsDriver)(nil)
