//go:build darwin && cgo

package taskterminal

/*
#include <stdlib.h>
#include "ghostty_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"
)

func openGhostty(ctx context.Context, path string) (Window, error) {
	if err := ctx.Err(); err != nil {
		return nil, NotLaunched(err)
	}
	command := C.CString(ghosttyCommand(path))
	defer C.free(unsafe.Pointer(command))
	handle := C.task_ghostty_open(command)
	defer C.task_ghostty_open_release(handle)
	w := &applicationInstance{handle: C.task_ghostty_open_instance(handle), terminal: "ghostty"}
	return finishGhosttyOpen(ctx, w, func() (int, int, int64) {
		var pid C.int
		var code C.long
		status := C.task_ghostty_open_poll(handle, &pid, &code)
		return int(status), int(pid), int64(code)
	})
}

func finishGhosttyOpen(ctx context.Context, w Window, poll func() (int, int, int64)) (Window, error) {
	_, err := waitGhosttyOpen(ctx, poll)
	if errors.Is(err, ErrWindowUnsupported) {
		// Native capability rejection precedes submission. Only this outcome
		// can discard the pending owner and select the standard launch route.
		w.Release()
		return nil, NotLaunched(err)
	}
	return w, err
}

func ghosttyCommand(path string) string {
	// The GUI inherits the headless Bot's NO_COLOR, not an interactive shell's
	// color choice. Let Ghostty's actual TERM/COLORTERM determine TUI colors.
	return "/usr/bin/env -u NO_COLOR /bin/sh " + quote(path)
}

// The common owner is retained before polling. Cancellation may return PID zero;
// a late native callback still fills that owner, while Launcher revokes the script.
func waitGhosttyOpen(ctx context.Context, poll func() (status, pid int, code int64)) (int, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, pid, code := poll()
		if err := ctx.Err(); err != nil {
			return pid, err
		}
		switch status {
		case 1:
			if pid <= 0 {
				return pid, ErrWindowIdentity
			}
			return pid, nil
		case -1:
			return pid, ErrWindowUnsupported
		case -2:
			return pid, ErrWindowPermission
		case -3:
			return pid, fmt.Errorf("Ghostty automation failed (%d)", code)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
