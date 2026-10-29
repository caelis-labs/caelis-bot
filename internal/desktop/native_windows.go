package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/windowsnotifications"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

var winUser = windows.NewLazySystemDLL("user32.dll")
var winGDI = windows.NewLazySystemDLL("gdi32.dll")
var winCommon = windows.NewLazySystemDLL("comctl32.dll")
var defSubclass = winCommon.NewProc("DefSubclassProc")

// Wails' ExStyle replaces, rather than adds to, its computed styles. Retain
// DirectComposition, tool-window and topmost flags when adding NOACTIVATE.
// Layered-window click-through is deliberately absent: it hides the composed
// WebView. The native silhouette region owns clipping and pointer targeting.
const windowsReadingStyle = 0x08000000 | 0x00010000 | 0x00000080 | 0x00000008
const windowsPetStyle = windowsReadingStyle | 0x00200000

func windowsCompatibility() error {
	v := windows.RtlGetVersion()
	if runtime.GOARCH != "amd64" || v.MajorVersion < 10 || v.BuildNumber < 19045 {
		return errors.New("Caelis Bot requires Windows 10 22H2 / Windows Server 2022 or later, x64, with a desktop session and WebView2 Runtime")
	}
	return nil
}
func windowsPreferredLanguages() []string {
	var name [85]uint16
	n, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName").Call(uintptr(unsafe.Pointer(&name[0])), 85)
	if n == 0 {
		return []string{"en"}
	}
	return []string{windows.UTF16ToString(name[:])}
}
func windowsOpenURL(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, 1)
}
func windowsRevealFile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("invalid reveal path")
	}
	c := exec.Command("explorer.exe", "/select,"+path)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
func windowsTrashFile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("invalid trash path")
	}
	names, err := windows.UTF16FromString(path)
	if err != nil {
		return err
	}
	names = append(names, 0)
	type operation struct {
		Window   uintptr
		Function uint32
		From, To *uint16
		Flags    uint16
		Aborted  int32
		Mappings uintptr
		Title    *uint16
	}
	value := operation{Function: 3, From: &names[0], Flags: 0x40 | 0x10 | 0x4 | 0x400}
	result, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW").Call(uintptr(unsafe.Pointer(&value)))
	if result != 0 || value.Aborted != 0 {
		return errors.New("file trash did not complete")
	}
	return nil
}

type windowsDriver struct {
	capture                    *windowsCapture
	taskThumbnails             map[string]windowsTaskThumbnail // guarded by Service.mu
	app                        *application.App
	pet, bubbleWindow, history *application.WebviewWindow
	taskWindow                 *application.WebviewWindow
	taskCount                  atomic.Int64
	taskExpanded               atomic.Bool
	taskLayout                 atomic.Value
	taskPeek                   atomic.Bool
	service                    *Service
	placement                  Placement
	maskBytes                  []byte
	callback                   uintptr
	hotkeys                    windowsHotkeys // native UI thread only
	stopped                    atomic.Bool
	maskReady                  bool
	maskReported               bool
	showReported               bool
	interaction                windowsPetInteraction
	contextSnapshot            atomic.Value
	contextStarted             time.Time
	contextRevision            uint64
	lastContext                string
	bubbleWanted               bool
	diagnostic                 func(string, string)
	notifications              *windowsnotifications.Manager
}

func newWindowsDriver(a *application.App, pet, bubble, history *application.WebviewWindow, s *Service) (*windowsDriver, error) {
	d := &windowsDriver{app: a, pet: pet, bubbleWindow: bubble, history: history, service: s, contextStarted: time.Now()}
	d.capture = &windowsCapture{driver: d, drafts: map[string]*windowsCaptureDraft{}, enabled: s.capture.preferences.Enabled}
	d.callback = syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam, id, data uintptr) uintptr {
		switch msg {
		case 0x113: // WM_TIMER: single waits for the OS double-click interval.
			if wparam == windowsPetContextTimer {
				d.publishContext()
				return 0
			}
			if wparam == windowsPetClickTimer {
				d.cancelClick(hwnd)
				if !d.stopped.Load() && d.placement.Visible {
					pet.ExecJS("window.dispatchEvent(new Event('pet-touch'))")
					go s.Activate()
				}
				return 0
			}
		case 0x216: // WM_MOVING: even a drag ending at its origin is not a click.
			d.beginPetMove()
		case 0x232: // WM_EXITSIZEMOVE
			d.endPetMove()
		case 0x211: // WM_ENTERMENULOOP
			d.interaction.Menu = true
			d.publishContext()
		case 0x212: // WM_EXITMENULOOP
			d.interaction.Menu = false
			d.publishContext()
		case 0x1F: // WM_CANCELMODE
			d.cancelClick(hwnd)
			d.interaction.cancel()
			d.endPetMove()
		case 0xA5, 0x7B: // WM_NCRBUTTONUP / WM_CONTEXTMENU
			d.cancelClick(hwnd)
			go d.openPetMenu()
			return 0
		case 0x312: // WM_HOTKEY: explicit user input, never renderer-triggered.
			logical := d.hotkeys.logical(int(wparam))
			if logical == 1 {
				go s.Activate()
			} else if logical == 2 {
				go s.ToggleTaskDock()
			} else if logical == 3 {
				go s.CaptureScreen()
			} else if logical == 4 {
				front, _, _ := winUser.NewProc("GetForegroundWindow").Call()
				go func() {
					if !d.capture.pinForeground(front) {
						s.PasteImage()
					}
				}()
			}
			return 0
		case 0x84:
			if !d.maskReady {
				return ^uintptr(0) // HTTRANSPARENT until the first rendered silhouette.
			}
			return 2 // WM_NCHITTEST: only the native silhouette is a caption.
		case 0x21:
			return 3 // WM_MOUSEACTIVATE: pet/reading never steals keyboard focus.
		case 0xA3:
			d.cancelClick(hwnd)
			pet.ExecJS("window.dispatchEvent(new Event('pet-touch'))")
			go s.RecallWindows()
			return 0 // WM_NCLBUTTONDBLCLK
		case 0xA1: // Default caption tracking owns pointer movement, no renderer authority.
			d.cancelClick(hwnd)
			d.interaction.begin()
			d.publishContext()
			beforeX, beforeY := pet.Position()
			result, _, _ := defSubclass.Call(hwnd, uintptr(msg), wparam, lparam)
			afterX, afterY := pet.Position()
			d.endPetMove()
			if d.interaction.click() && beforeX == afterX && beforeY == afterY {
				interval, _, _ := winUser.NewProc("GetDoubleClickTime").Call()
				winUser.NewProc("SetTimer").Call(hwnd, windowsPetClickTimer, interval, 0)
			} else {
				height := d.primaryHeight()
				go s.moved(float64(afterX), height-float64(afterY)-BaseHeight*d.placement.Scale)
			}
			return result
		case 0x02:
			d.cancelClick(hwnd)
			d.interaction.cancel()
			winUser.NewProc("KillTimer").Call(hwnd, windowsPetContextTimer)
			d.stopped.Store(true) // WM_DESTROY
		}
		result, _, _ := defSubclass.Call(hwnd, uintptr(msg), wparam, lparam)
		return result
	})
	var hookErr error
	application.InvokeSync(func() {
		hwnd := uintptr(pet.NativeWindow())
		if hwnd == 0 {
			hookErr = errors.New("pet native window is not ready")
			return
		}
		if ok, _, err := winCommon.NewProc("SetWindowSubclass").Call(hwnd, d.callback, 1, 0); ok == 0 {
			hookErr = fmt.Errorf("install pet input handler: %w", err)
			return
		}
		winUser.NewProc("SetTimer").Call(hwnd, windowsPetContextTimer, 125, 0)
	})
	return d, hookErr
}

const windowsPetClickTimer = 0xCB01

func (d *windowsDriver) cancelClick(hwnd uintptr) {
	winUser.NewProc("KillTimer").Call(hwnd, windowsPetClickTimer)
}

func (d *windowsDriver) openPetMenu() {
	// Read product state before entering the UI thread: Service.apply can itself
	// wait for that thread while holding its placement mutex.
	language := d.service.LanguagePreferences()
	visible := d.service.Placement().Visible
	menu := application.NewContextMenu("pet-actions")
	label := func(key string) string { return i18n.Text(language.Locale, "native."+key, nil) }
	menu.Add(label("chat")).OnClick(func(*application.Context) { go d.service.OpenHistory() })
	key := "showPet"
	if visible {
		key = "hidePet"
	}
	menu.Add(label(key)).OnClick(func(*application.Context) { go func() { logError(d.service.SetVisible(!visible)) }() })
	d.app.ContextMenu.Add("pet-actions", menu)
	if !d.stopped.Load() {
		d.pet.OpenContextMenu(&application.ContextMenuData{Id: "pet-actions"})
	}
}
func (d *windowsDriver) primaryHeight() float64 {
	p := d.app.Screen.GetPrimary()
	if p == nil {
		return 1080
	}
	return float64(p.Size.Height)
}
func (d *windowsDriver) screens() []Rect {
	height := d.primaryHeight()
	var result []Rect
	for _, s := range d.app.Screen.GetAll() {
		r := s.WorkArea
		result = append(result, Rect{float64(r.X), height - float64(r.Y+r.Height), float64(r.Width), float64(r.Height)})
	}
	return result
}
func (d *windowsDriver) apply(p Placement) {
	application.InvokeSync(func() {
		if d.stopped.Load() {
			return
		}
		d.placement = p
		if !p.Visible {
			d.cancelClick(uintptr(d.pet.NativeWindow()))
			d.interaction.cancel()
		}
		w, h := int(math.Round(BaseWidth*p.Scale)), int(math.Round(BaseHeight*p.Scale))
		d.pet.SetSize(w, h)
		d.pet.SetPosition(int(math.Round(p.X)), int(math.Round(d.primaryHeight()-p.Y-float64(h))))
		d.layout()
		d.updateMask()
		data, _ := json.Marshal(p.Visible)
		for _, w := range []*application.WebviewWindow{d.pet, d.bubbleWindow} {
			w.ExecJS("window.dispatchEvent(new CustomEvent('pet-visibility',{detail:" + string(data) + "}))")
		}
		if p.Visible {
			showWindowsWindow(d.pet)
			d.refreshTaskDock()
			// The renderer only sends SetBubbleVisible when its content intent
			// changes. Restore unchanged content after a native hide/show cycle.
			if d.bubbleWanted && !d.interaction.Dragging {
				showWindowsWindow(d.bubbleWindow)
			}
			if !d.showReported && d.diagnostic != nil {
				d.showReported = true
				d.diagnostic("pet_show", "native surface shown without activation")
			}
		} else {
			d.taskPeek.Store(false)
			d.taskExpanded.Store(false)
			d.pet.Hide()
			d.bubbleWindow.ExecJS("window.dispatchEvent(new Event('bubble-hidden'))")
			d.bubbleWindow.Hide()
			if d.taskWindow != nil {
				d.taskWindow.Hide()
			}
		}
		d.publishContext()
	})
}
func (d *windowsDriver) layout() {
	x, y := d.pet.Position()
	w, h := d.pet.Size()
	screen := d.app.Screen.ScreenNearestDipPoint(application.Point{X: x, Y: y})
	_, bh := d.bubbleWindow.Size()
	bx := x + w/2 - 180
	by := y - bh - 8
	if screen != nil {
		r := screen.WorkArea
		bx = max(r.X, min(bx, r.X+r.Width-360))
		by = max(r.Y, by)
	}
	d.bubbleWindow.SetPosition(bx, by)
	if d.taskWindow != nil {
		area := application.Rect{Width: 800, Height: 600}
		if screen != nil {
			area = screen.WorkArea
		}
		layout := windowsTaskPlacement(application.Rect{X: x, Y: y, Width: w, Height: h}, area, int(d.taskCount.Load()), d.taskExpanded.Load())
		d.taskLayout.Store(layout)
		d.taskWindow.SetSize(layout.Width, layout.Height)
		d.taskWindow.SetPosition(layout.X, layout.Y)
	}
}

func (d *windowsDriver) toggleTaskDock() {
	d.setTaskDockExpanded(!d.taskExpanded.Load(), true)
}
func (d *windowsDriver) setTaskDockExpanded(expanded, focus bool) {
	application.InvokeSync(func() {
		if d.taskWindow == nil || d.stopped.Load() {
			return
		}
		d.changeTaskDockExpansion(expanded, focus)
		d.refreshTaskDock()
		if expanded && focus {
			d.taskWindow.Focus()
		}
	})
}
func (d *windowsDriver) changeTaskDockExpansion(expanded, explicit bool) {
	d.taskExpanded.Store(expanded)
	if !expanded {
		d.taskPeek.Store(false)
	} else if explicit {
		d.taskPeek.Store(true)
	}
}

// Called only on the native UI thread. Read-only task refreshes never focus a window.
func (d *windowsDriver) refreshTaskDock() {
	if d.taskWindow == nil || d.stopped.Load() {
		return
	}
	d.layout()
	if d.taskDockVisible() {
		showWindowsWindow(d.taskWindow)
	} else {
		d.taskWindow.Hide()
	}
	d.taskWindow.ExecJS("window.dispatchEvent(new Event('tasks-changed'))")
}
func (d *windowsDriver) taskDockVisible() bool {
	return (d.placement.Visible || d.taskPeek.Load()) && !d.interaction.Dragging && (d.taskCount.Load() > 0 || d.taskExpanded.Load())
}
func (d *windowsDriver) tasks(value string) {
	var tasks []api.TaskPreview
	if value != "" && json.Unmarshal([]byte(value), &tasks) == nil {
		ids := make([]string, len(tasks))
		for i := range tasks {
			ids[i] = tasks[i].ID
		}
		d.taskCount.Store(int64(len(tasks)))
		d.pruneTaskThumbnails(ids)
	}
	if d.taskWindow != nil {
		application.InvokeAsync(d.refreshTaskDock)
	}
}
func (d *windowsDriver) taskFailure(message string)    { d.taskMessage(message) }
func (d *windowsDriver) taskOpening(_, message string) { d.taskMessage(message) }
func (d *windowsDriver) taskMessage(message string) {
	if d.taskWindow != nil {
		b, _ := json.Marshal(message)
		d.taskWindow.ExecJS("window.dispatchEvent(new CustomEvent('task-notice',{detail:" + string(b) + "}))")
	}
}
func (d *windowsDriver) taskTransitions(string)                { d.tasks("") }
func (d *windowsDriver) prepareWindowRecall() bool             { return !d.stopped.Load() }
func (d *windowsDriver) approval()                             { go d.service.OpenHistory() }
func (d *windowsDriver) registerShortcut(s Shortcut) error     { return d.registerHotkey(1, s) }
func (d *windowsDriver) registerTaskShortcut(s Shortcut) error { return d.registerHotkey(2, s) }
func (d *windowsDriver) registerHotkey(id int, s Shortcut) error {
	var result error
	application.InvokeSync(func() {
		hwnd := uintptr(d.pet.NativeWindow())
		result = d.hotkeys.replace(id, s, func(id int, mods, key uint32) bool {
			ok, _, _ := winUser.NewProc("RegisterHotKey").Call(hwnd, uintptr(id), uintptr(mods), uintptr(key))
			return ok != 0
		}, func(id int) bool {
			ok, _, _ := winUser.NewProc("UnregisterHotKey").Call(hwnd, uintptr(id))
			return ok != 0
		})
	})
	return result
}
func (d *windowsDriver) bubble(show bool) {
	application.InvokeSync(func() {
		d.bubbleWanted = show
		if show && d.placement.Visible && !d.interaction.Dragging {
			d.layout()
			showWindowsWindow(d.bubbleWindow)
		} else {
			d.bubbleWindow.ExecJS("window.dispatchEvent(new Event('bubble-hidden'))")
			d.bubbleWindow.Hide()
		}
	})
}
func (d *windowsDriver) expandBubble(expanded bool) {
	event := "bubble-collapse"
	if expanded {
		event = "bubble-expand"
	}
	d.bubbleWindow.ExecJS("window.dispatchEvent(new Event('" + event + "'))")
}
func (d *windowsDriver) bubbleHeight(h int) {
	application.InvokeSync(func() { d.bubbleWindow.SetSize(360, h); d.layout() })
}
func (d *windowsDriver) activity(value string) {
	data, _ := json.Marshal(value)
	d.pet.ExecJS("window.dispatchEvent(new CustomEvent('pet-activity',{detail:" + string(data) + "}))")
}
func (d *windowsDriver) gesture(value string) {
	data, _ := json.Marshal(value)
	d.pet.ExecJS("window.dispatchEvent(new CustomEvent('pet-gesture',{detail:" + string(data) + "}))")
}
func (d *windowsDriver) mask(mask []byte) {
	copyMask := append([]byte(nil), mask...)
	application.InvokeSync(func() { d.maskBytes = copyMask; d.updateMask() })
}
func (d *windowsDriver) updateMask() {
	if len(d.maskBytes) != int(BaseWidth*BaseHeight) || d.stopped.Load() {
		return
	}
	hwnd := uintptr(d.pet.NativeWindow())
	var client struct{ Left, Top, Right, Bottom int32 }
	if ok, _, _ := winUser.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&client))); ok == 0 {
		return
	}
	sx, sy := float64(client.Right)/BaseWidth, float64(client.Bottom)/BaseHeight
	rects := windowsMaskRects(d.maskBytes, sx, sy)
	if len(rects) == 0 {
		d.maskReady = false
		return
	}
	// RGNDATA is one bounded native call per animated mask, not one call per pixel.
	bytes := make([]byte, 32+16*len(rects))
	header := (*[8]uint32)(unsafe.Pointer(&bytes[0]))
	header[0] = 32
	header[1] = 1
	header[2] = uint32(len(rects))
	header[3] = uint32(16 * len(rects))
	header[6] = uint32(client.Right)
	header[7] = uint32(client.Bottom)
	target := unsafe.Slice((*windowsMaskRect)(unsafe.Pointer(&bytes[32])), len(rects))
	copy(target, rects)
	region, _, _ := winGDI.NewProc("ExtCreateRegion").Call(0, uintptr(len(bytes)), uintptr(unsafe.Pointer(&bytes[0])))
	if region == 0 {
		return
	}
	ok, _, _ := winUser.NewProc("SetWindowRgn").Call(hwnd, region, 1)
	if ok == 0 {
		winGDI.NewProc("DeleteObject").Call(region)
		return
	}
	d.maskReady = true
	if !d.maskReported && d.diagnostic != nil {
		d.maskReported = true
		d.diagnostic("pet_mask_ready", fmt.Sprintf("runs=%d", len(rects)))
	}
}

func showWindowsSurface(hwnd uintptr) error {
	if hwnd == 0 {
		return errors.New("native surface is not ready")
	}
	const flags = 0x0001 | 0x0002 | 0x0004 | 0x0010 | 0x0040 // NOSIZE, NOMOVE, NOZORDER, NOACTIVATE, SHOWWINDOW.
	if ok, _, err := winUser.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, 0, 0, flags); ok == 0 {
		return fmt.Errorf("show native surface: %w", err)
	}
	return nil
}

func showWindowsWindow(w *application.WebviewWindow) {
	// Wails Show also wakes its WebView controller, but SW_SHOW activates a
	// normal HWND. Suppress activation for the entire native call, not after it.
	showWindowsWithoutActivation(uintptr(w.NativeWindow()), func() { w.Show() })
	// The first ShowWindow obeys STARTUPINFO rather than SW_SHOW. Hidden launch
	// parameters must not suppress a user's explicit/persisted visible surface.
	// SetWindowPos shows the owned HWND without taking keyboard focus.
	logError(showWindowsSurface(uintptr(w.NativeWindow())))
}

// Only explicit user navigation/capture calls this path. Passive pet, bubble
// and task refreshes must continue using showWindowsWindow. Never attach the
// foreground process's input queue: a stalled external window would then block
// our UI thread during activation. Wails transfers focus to the ready WebView.
func activateWindowsWindow(w *application.WebviewWindow) {
	w.UnMinimise()
	showWindowsWindow(w)
	w.Focus()
}

func showWindowsWithoutActivation(hwnd uintptr, show func()) {
	const noActivate = 0x08000000
	const exStyle = ^uintptr(19) // GWL_EXSTYLE = -20
	style, _, _ := winUser.NewProc("GetWindowLongPtrW").Call(hwnd, exStyle)
	winUser.NewProc("SetWindowLongPtrW").Call(hwnd, exStyle, style|noActivate)
	defer winUser.NewProc("SetWindowLongPtrW").Call(hwnd, exStyle, style)
	show()
}

type windowsMaskRect struct{ Left, Top, Right, Bottom int32 }

func windowsMaskRects(mask []byte, sx, sy float64) []windowsMaskRect {
	if len(mask) != int(BaseWidth*BaseHeight) || sx <= 0 || sy <= 0 {
		return nil
	}
	var rects []windowsMaskRect
	for y := 0; y < int(BaseHeight); y++ {
		// WebGL's mask is bottom-up; Windows regions use top-down client pixels.
		row := (int(BaseHeight) - 1 - y) * int(BaseWidth)
		for x := 0; x < int(BaseWidth); {
			if mask[row+x] == 0 {
				x++
				continue
			}
			start := x
			for x < int(BaseWidth) && mask[row+x] != 0 {
				x++
			}
			rects = append(rects, windowsMaskRect{int32(math.Floor(float64(start) * sx)), int32(math.Floor(float64(y) * sy)), int32(math.Ceil(float64(x) * sx)), int32(math.Ceil(float64(y+1) * sy))})
		}
	}
	return rects
}
func (d *windowsDriver) stop() {
	if d.notifications != nil {
		d.notifications.Close()
	}
	if d.capture != nil {
		d.capture.closeWindows()
	}
	application.InvokeSync(func() {
		d.stopped.Store(true)
		d.cancelClick(uintptr(d.pet.NativeWindow()))
		d.interaction.cancel()
		winUser.NewProc("KillTimer").Call(uintptr(d.pet.NativeWindow()), windowsPetContextTimer)
		for _, entry := range d.hotkeys.entries {
			winUser.NewProc("UnregisterHotKey").Call(uintptr(d.pet.NativeWindow()), uintptr(entry.id))
		}
		winCommon.NewProc("RemoveWindowSubclass").Call(uintptr(d.pet.NativeWindow()), d.callback, 1)
		d.pet.Hide()
		d.bubbleWindow.Hide()
		if d.taskWindow != nil {
			d.taskWindow.Hide()
		}
	})
}

var _ driver = (*windowsDriver)(nil)

// Configuration is text, never an association-driven executable open.
func windowsOpenTextFile(path string) error {
	directory, e := windows.GetSystemDirectory()
	if e != nil {
		return e
	}
	cmd := exec.Command(filepath.Join(directory, "notepad.exe"), path)
	if e = cmd.Start(); e != nil {
		return e
	}
	return cmd.Process.Release()
}
