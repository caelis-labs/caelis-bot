//go:build darwin && cgo

package taskterminal

/*
#include <stdlib.h>
#include "ghostty_darwin.h"
*/
import "C"

import (
	"context"
	"fmt"
	"time"
	"unsafe"
)

func openGhostty(ctx context.Context, path string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	command := C.CString(ghosttyCommand(path))
	defer C.free(unsafe.Pointer(command))
	handle := C.task_ghostty_open(command)
	defer C.task_ghostty_open_release(handle)
	return waitGhosttyOpen(ctx, func() (int, int, int64) {
		var pid C.int
		var code C.long
		status := C.task_ghostty_open_poll(handle, &pid, &code)
		return int(status), int(pid), int64(code)
	})
}

func ghosttyCommand(path string) string {
	// The GUI inherits the headless Bot's NO_COLOR, not an interactive shell's
	// color choice. Let Ghostty's actual TERM/COLORTERM determine TUI colors.
	return "/usr/bin/env -u NO_COLOR /bin/sh " + quote(path)
}

// Cancellation returns the launched identity even if the event reply is still
// pending. Launcher revokes the script's token and reconciles an accepted client;
// a late permission grant cannot start the task again.
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
		case -4:
			return pid, ErrWindowActivation
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
