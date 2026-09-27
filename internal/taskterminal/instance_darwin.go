//go:build darwin && cgo

package taskterminal

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include <stdlib.h>
#include "instance_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unsafe"
)

// applicationInstance owns only the GUI launched for this card. The shared
// Runtime/Worker is not its child and is never stopped by this handle.
type applicationInstance struct {
	handle      unsafe.Pointer
	terminal    string
	clientPID   int
	clientBirth uint64
	clientEnded bool
}

func OpenWindow(ctx context.Context, preference, path string) (Window, error) {
	bundle := BundleID(preference)
	if bundle == "" {
		return nil, ErrUnsupportedDefault
	}
	if preference == "ghostty" {
		// Optional creation enhancement. Management below is identical for
		// every terminal. Fallback is allowed only before anything launched.
		pid, err := openGhostty(ctx, path)
		if pid > 0 {
			id := C.CString(bundle)
			defer C.free(unsafe.Pointer(id))
			w := &applicationInstance{handle: C.bot_terminal_instance_adopt(C.int(pid), id), terminal: preference}
			return w, err
		}
		if !errors.Is(err, ErrWindowUnsupported) {
			return nil, err
		}
	}
	return OpenApplication(ctx, bundle, preference, path)
}

// OpenApplication supports a system-associated terminal without a brand adapter.
// Accepting a script/document is the terminal's capability, not a Bot guarantee.
func OpenApplication(ctx context.Context, bundle, name, path string) (Window, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bundle == "" || strings.ContainsRune(bundle, 0) || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return nil, ErrWindowIdentity
	}
	b, p := C.CString(bundle), C.CString(path)
	defer C.free(unsafe.Pointer(b))
	defer C.free(unsafe.Pointer(p))
	w := &applicationInstance{handle: C.bot_terminal_instance_open(b, p), terminal: name}
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch C.bot_terminal_instance_ready(w.handle) {
		case 1:
			return w, nil
		case -1:
			return w, ErrWindowIdentity
		}
		select {
		case <-ctx.Done():
			return w, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (w *applicationInstance) keepUnconfirmedLaunch() bool { return true }
func (w *applicationInstance) Confirm(ctx context.Context, pid int) error {
	if pid <= 0 {
		return ErrWindowIdentity
	}
	o, err := w.Observe(ctx)
	if err != nil {
		return err
	}
	if o.State == WindowClosed {
		return ErrWindowIdentity
	}
	// The receipt PID survives exec into the attach client. Track only that
	// process, not a terminal's TTY, window or detached session server.
	state, birth := terminalClientState(pid, 0)
	if state >= 0 {
		w.clientPID, w.clientBirth, w.clientEnded = pid, birth, state == 0
	}
	return nil
}

func (w *applicationInstance) OpenDocument(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return ErrWindowIdentity
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if C.bot_terminal_instance_open_document(w.handle, p) != 1 {
		if o, err := w.Observe(ctx); err == nil && o.State == WindowClosed {
			return ErrWindowOpenCancelled
		}
		return ErrWindowIdentity
	}
	w.clientPID, w.clientBirth, w.clientEnded = 0, 0, false
	return nil
}
func (w *applicationInstance) DocumentResult(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	switch C.bot_terminal_instance_document_result(w.handle) {
	case 0, 1:
		return false, nil
	case 2:
		return true, nil
	case 3:
		return true, ErrWindowOpenCancelled
	case 4:
		return true, ErrWindowNotConnected
	case -2:
		return true, ErrWindowPermission
	case -3:
		return true, ErrWindowUnsupported
	default:
		return true, ErrWindowControl
	}
}

func terminalClientState(pid int, expected uint64) (int, uint64) {
	var birth C.uint64_t
	state := C.bot_terminal_client_state(C.int(pid), C.uint64_t(expected), &birth)
	return int(state), uint64(birth)
}
func (w *applicationInstance) Observe(ctx context.Context) (WindowObservation, error) {
	o := WindowObservation{State: WindowUnknown, InputEpoch: InputEpoch()}
	if err := ctx.Err(); err != nil {
		return o, err
	}
	var state, pid C.int
	var birth C.uint64_t
	result := C.bot_terminal_instance_observe(w.handle, &state, &pid, &birth)
	if result == 0 {
		return o, ErrWindowObservationPending
	}
	if result != 1 {
		return o, ErrWindowIdentity
	}
	o.State = map[C.int]WindowState{1: WindowClosed, 2: WindowCollapsed, 3: WindowBackground, 4: WindowForeground}[state]
	o.AppActive = o.State == WindowForeground
	o.Settled, o.CanCollapse = true, true
	if w.clientPID > 0 && !w.clientEnded {
		state, _ := terminalClientState(w.clientPID, w.clientBirth)
		w.clientEnded = state == 0 // Unknown never authorizes replacement.
	}
	o.ClientEnded = w.clientEnded
	return o, nil
}
func (w *applicationInstance) Apply(ctx context.Context, command WindowCommand) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	native := 2
	switch command {
	case CollapseWindow:
		native = 3
	case ShowWindow, FocusWindow:
	default:
		return ErrWindowUnsupported
	}
	switch C.bot_terminal_instance_apply(w.handle, C.int(native)) {
	case 1:
		return nil
	case -2:
		return ErrWindowPermission
	case -1:
		return ErrWindowControl
	default:
		return ErrWindowControl
	}
}
func (w *applicationInstance) Dismiss(ctx context.Context) error {
	o, err := w.Observe(ctx)
	if err != nil || o.State == WindowClosed {
		return err
	}
	if C.bot_terminal_instance_request_close(w.handle) != 1 {
		return ErrWindowControl
	}
	// Normal quit may need terminal confirmation. Never force it or claim the
	// card closed until the owned app really exits. A failed close retains it.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		o, err = w.Observe(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrWindowClosePending
		}
		if err != nil && !errors.Is(err, ErrWindowObservationPending) {
			return err
		}
		if err == nil && o.State == WindowClosed {
			return nil
		}
		switch C.bot_terminal_instance_close_result(w.handle) {
		case 3:
			return ErrWindowCloseCancelled
		case 4:
			return ErrWindowClosePending
		case -2:
			return ErrWindowPermission
		case -1:
			return ErrWindowControl
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ErrWindowClosePending
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (w *applicationInstance) Preview(ctx context.Context) (PreviewSource, error) {
	if err := ctx.Err(); err != nil {
		return PreviewSource{}, err
	}
	var state, pid C.int
	var birth C.uint64_t
	if C.bot_terminal_instance_observe(w.handle, &state, &pid, &birth) != 1 || state != 4 {
		return PreviewSource{}, ErrWindowIdentity
	}
	return PreviewSource{PID: int(pid), Birth: uint64(birth), Terminal: w.terminal}, nil
}
func (w *applicationInstance) Release() {
	if w.handle != nil {
		C.bot_terminal_instance_release(w.handle)
		w.handle = nil
	}
}

// InputEpoch observes only an input counter, never keystrokes or content.
func InputEpoch() uint64 { return uint64(C.bot_terminal_input_epoch()) }
