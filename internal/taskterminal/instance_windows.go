package taskterminal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var consoleMu sync.Mutex
var terminalKernel = windows.NewLazySystemDLL("kernel32.dll")
var terminalUser = windows.NewLazySystemDLL("user32.dll")

type consoleWindow struct {
	mu        sync.Mutex
	pid       int
	process   windows.Handle
	hwnd      uintptr
	hostPID   uint32
	hostBirth uint64
	observer  bool
}

// LaunchConsole delivers exactly one claimed argv to a new native console.
// It is outside the resident App Server's Job: closing Bot releases handles,
// never kills this client, terminal host, or unrelated workers.
func LaunchConsole(ctx context.Context, path string) (Window, error) {
	return launchConsole(ctx, path, true)
}
func launchConsole(ctx context.Context, path string, newConsole bool) (Window, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, NotLaunched(err)
	}
	var v windowsLaunch
	if len(data) > 65536 || json.Unmarshal(data, &v) != nil || v.Version != 1 || !filepath.IsAbs(v.Command.Binary) || !filepath.IsAbs(v.Command.Directory) || filepath.Clean(v.Pending) != filepath.Join(filepath.Dir(path), "pending") || v.AcceptedPrefix != filepath.Join(filepath.Dir(path), "accepted-") {
		return nil, NotLaunched(ErrWindowIdentity)
	}
	if err = ctx.Err(); err != nil {
		return nil, NotLaunched(err)
	}
	// CreateProcess without STARTF_USESTDHANDLES lets Windows initialize real
	// console handles. os/exec's nil stdio opens NUL and breaks interactive TUIs.
	if !strings.EqualFold(filepath.Ext(v.Command.Binary), ".exe") {
		return nil, NotLaunched(ErrWindowIdentity)
	}
	argv := []string{syscall.EscapeArg(v.Command.Binary)}
	for _, arg := range v.Command.Args {
		argv = append(argv, syscall.EscapeArg(arg))
	}
	command, err := windows.UTF16PtrFromString(strings.Join(argv, " "))
	if err != nil {
		return nil, NotLaunched(err)
	}
	binary, err := windows.UTF16PtrFromString(v.Command.Binary)
	if err != nil {
		return nil, NotLaunched(err)
	}
	directory, err := windows.UTF16PtrFromString(v.Command.Directory)
	if err != nil {
		return nil, NotLaunched(err)
	}
	environment, err := consoleEnvironment(v.Command.Env)
	if err != nil {
		return nil, NotLaunched(err)
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT)
	if newConsole {
		flags |= windows.CREATE_NEW_CONSOLE
	}
	if err = windows.CreateProcess(binary, command, nil, nil, false, flags, &environment[0], directory, &startup, &process); err != nil {
		return nil, NotLaunched(err)
	}
	defer windows.CloseHandle(process.Thread)
	fail := func(e error) (Window, error) {
		_ = windows.TerminateProcess(process.Process, 1)
		_, _ = windows.WaitForSingleObject(process.Process, 5000)
		windows.CloseHandle(process.Process)
		return nil, NotLaunched(e)
	}
	if err = ctx.Err(); err == nil {
		err = writeConsoleIdentity(path, process.Process, int(process.ProcessId))
	}
	if err == nil {
		err = os.Rename(v.Pending, v.AcceptedPrefix+strconv.Itoa(int(process.ProcessId)))
	}
	if err != nil {
		return fail(err)
	}
	// After the receipt commits, errors are uncertain and cannot authorize replay.
	w := &consoleWindow{pid: int(process.ProcessId), process: process.Process, observer: v.Observer}
	n, _, resumeErr := terminalKernel.NewProc("ResumeThread").Call(uintptr(process.Thread))
	if n == 0xffffffff {
		_ = windows.TerminateProcess(process.Process, 1)
		return w, resumeErr
	}
	if !newConsole {
		return w, nil
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(30 * time.Millisecond)
	defer tick.Stop()
	for {
		w.bindConsoleWindow()
		if w.hwnd != 0 {
			return w, nil
		}
		select {
		case <-ctx.Done():
			return w, ctx.Err()
		case <-deadline.C:
			return w, nil
		case <-tick.C:
		}
	}
}

// The native environment is double-NUL terminated, sorted and case-insensitive.
// The only target override is CODEX_HOME; receipt paths never become shell text.
func consoleEnvironment(overrides map[string]string) ([]uint16, error) {
	env := map[string]string{}
	for _, pair := range runtimeenv.Clean(os.Environ()) {
		i := strings.IndexByte(pair, '=')
		if i <= 0 {
			continue
		}
		env[strings.ToUpper(pair[:i])] = pair[i+1:]
	}
	for k, v := range overrides {
		if k != "CODEX_HOME" {
			return nil, ErrWindowIdentity
		}
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var block []uint16
	for _, k := range keys {
		v, err := windows.UTF16FromString(k + "=" + env[k])
		if err != nil {
			return nil, err
		}
		block = append(block, v...)
	}
	return append(block, 0), nil
}
func consoleHandle(pid uint32) uintptr {
	consoleMu.Lock()
	defer consoleMu.Unlock()
	// Never detach an existing inherited console (e.g. a CLI test runner).
	if h, _, _ := terminalKernel.NewProc("GetConsoleWindow").Call(); h != 0 {
		return 0
	}
	if ok, _, _ := terminalKernel.NewProc("AttachConsole").Call(uintptr(pid)); ok == 0 {
		return 0
	}
	defer terminalKernel.NewProc("FreeConsole").Call()
	h, _, _ := terminalKernel.NewProc("GetConsoleWindow").Call()
	return consoleDisplayWindow(h)
}

// Windows Terminal reparents the ConPTY message window to its visible HWND.
// Follow only that OS-owned chain, never a title, foreground window or PID scan.
// https://github.com/microsoft/terminal/blob/main/src/cascadia/TerminalApp/TerminalPage.cpp
func consoleDisplayWindow(console uintptr) uintptr {
	if console == 0 {
		return 0
	}
	root, _, _ := terminalUser.NewProc("GetAncestor").Call(console, 3) // GA_ROOTOWNER
	if root == 0 {
		return 0
	}
	if visible, _, _ := terminalUser.NewProc("IsWindowVisible").Call(root); visible == 0 {
		return 0
	}
	return root
}

func consoleHostIdentity(hwnd uintptr) (uint32, uint64) {
	var pid uint32
	terminalUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return 0, 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return 0, 0
	}
	defer windows.CloseHandle(h)
	if state, err := windows.WaitForSingleObject(h, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		return 0, 0
	}
	birth, err := processBirth(h)
	if err != nil {
		return 0, 0
	}
	return pid, birth
}

func (w *consoleWindow) bindConsoleWindow() {
	if w.hwnd != 0 {
		return
	}
	hwnd := consoleHandle(uint32(w.pid))
	pid, birth := consoleHostIdentity(hwnd)
	if pid != 0 && birth != 0 {
		w.hwnd, w.hostPID, w.hostBirth = hwnd, pid, birth
	}
}
func (w *consoleWindow) Confirm(ctx context.Context, pid int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if pid != w.pid || w.process == 0 {
		return ErrWindowIdentity
	}
	return nil
}
func (w *consoleWindow) Release() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.process != 0 {
		windows.CloseHandle(w.process)
		w.process = 0
	}
}
func (w *consoleWindow) keepUnconfirmedLaunch() bool { return true }
func (w *consoleWindow) Observe(ctx context.Context) (WindowObservation, error) {
	return w.observe(ctx, w.inspectWindow)
}

// Keep native mapping facts separate from the original client's lifecycle.
// A confirmed live client can precede its host's first visible HWND.
func (w *consoleWindow) observe(ctx context.Context, inspect func() (WindowObservation, bool)) (WindowObservation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	o := WindowObservation{State: WindowUnknown, InputEpoch: InputEpoch()}
	if err := ctx.Err(); err != nil {
		return o, err
	}
	if w.process == 0 {
		return o, ErrWindowIdentity
	}
	n, err := windows.WaitForSingleObject(w.process, 0)
	if err != nil {
		return o, err
	}
	if n == windows.WAIT_OBJECT_0 {
		o.State = WindowClosed
		o.ClientEnded = true
		o.Settled = true
		return o, nil
	}
	state, valid := inspect()
	if !valid {
		if w.hwnd == 0 {
			return o, ErrWindowObservationPending
		}
		return o, ErrWindowUnsupported
	}
	state.InputEpoch = o.InputEpoch
	return state, nil
}

func (w *consoleWindow) inspectWindow() (WindowObservation, bool) {
	o := WindowObservation{State: WindowUnknown}
	if !w.validWindow() {
		return o, false
	}
	o.CanCollapse = true
	o.Settled = true
	if yes, _, _ := terminalUser.NewProc("IsIconic").Call(w.hwnd); yes != 0 {
		o.State = WindowCollapsed
		return o, true
	}
	front, _, _ := terminalUser.NewProc("GetForegroundWindow").Call()
	o.AppActive = front == w.hwnd
	if o.AppActive {
		o.State = WindowForeground
	} else {
		o.State = WindowBackground
	}
	return o, true
}
func (w *consoleWindow) validWindow() bool {
	// A hosted terminal may publish the observer receipt before it finishes
	// reparenting ConPTY. Reconcile may bind the original client once ready.
	w.bindConsoleWindow()
	if w.hwnd == 0 {
		return false
	}
	if yes, _, _ := terminalUser.NewProc("IsWindowVisible").Call(w.hwnd); yes == 0 {
		return false
	}
	// Re-attest the OS console association instead of matching window titles/PIDs.
	pid, birth := consoleHostIdentity(w.hwnd)
	return pid == w.hostPID && birth == w.hostBirth && consoleHandle(uint32(w.pid)) == w.hwnd
}
func (w *consoleWindow) Apply(ctx context.Context, command WindowCommand) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.process == 0 || !w.validWindow() {
		return ErrWindowUnsupported
	}
	switch command {
	case CollapseWindow:
		terminalUser.NewProc("ShowWindow").Call(w.hwnd, 6)
	case ShowWindow, FocusWindow:
		terminalUser.NewProc("ShowWindow").Call(w.hwnd, 9)
		if ok, _, _ := terminalUser.NewProc("SetForegroundWindow").Call(w.hwnd); ok == 0 {
			return ErrWindowActivation
		}
	default:
		return ErrWindowUnsupported
	}
	return nil
}
func (w *consoleWindow) Dismiss(ctx context.Context) error {
	w.mu.Lock()
	if w.process == 0 {
		w.mu.Unlock()
		return ErrWindowIdentity
	}
	if n, _ := windows.WaitForSingleObject(w.process, 0); n == windows.WAIT_OBJECT_0 {
		w.mu.Unlock()
		return nil
	}
	if !w.validWindow() {
		w.mu.Unlock()
		return ErrWindowUnsupported
	}
	h, p := w.hwnd, w.process
	w.mu.Unlock()
	if ok, _, _ := terminalUser.NewProc("PostMessageW").Call(h, 0x10, 0, 0); ok == 0 {
		return ErrWindowControl
	}
	for {
		if n, err := windows.WaitForSingleObject(p, 50); err != nil {
			return err
		} else if n == windows.WAIT_OBJECT_0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(ErrWindowClosePending, err)
		}
	}
}
func InputEpoch() uint64 {
	var v struct{ Size, Tick uint32 }
	v.Size = uint32(unsafe.Sizeof(v))
	if ok, _, _ := terminalUser.NewProc("GetLastInputInfo").Call(uintptr(unsafe.Pointer(&v))); ok == 0 {
		return 0
	}
	return uint64(v.Tick)
}

var _ ControlledWindow = (*consoleWindow)(nil)
