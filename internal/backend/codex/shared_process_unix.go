//go:build darwin || linux

package codex

import (
	"errors"
	"syscall"
)

// Signal zero observes existence only. A reused or inaccessible PID is treated
// as live/unknown; Bot never sends a terminating signal to a shared process.
func processGone(pid int) bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
