package codex

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// This runs only in the dedicated native watchdog, before its owned launch.
// Reparented tools must remain waitable there instead of leaking to a host or
// container init whose asynchronous reaping cannot form our stop receipt.
func prepareWatchdogReaping() error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return errors.Join(errors.New("owned watchdog descendant reaper unavailable"), err)
	}
	return nil
}

// Reap only the already captured, exited process handles after the native root
// Cmd has been waited. No numeric PID or newly discovered orphan grants any
// authority, and waitid cannot consume an unrelated process's exit status.
func (o *ownedTools) reapWatchdogChildren() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	var result error
	for pid, born := range o.children {
		fd, ok := o.handles[pid]
		if !ok {
			result = errors.Join(result, errors.New("owned descendant reaping handle unavailable"))
			continue
		}
		exited, err := linuxHandleExited(fd)
		if err != nil || !exited {
			result = errors.Join(result, err, errors.New("owned descendant reaping unconfirmed"))
			continue
		}
		for {
			var info unix.Siginfo
			err = unix.Waitid(unix.P_PIDFD, fd, &info, unix.WEXITED, nil)
			if !errors.Is(err, unix.EINTR) {
				break
			}
		}
		if errors.Is(err, unix.ECHILD) {
			// An original parent may have reaped a tool before stopping. Prove
			// that original identity is gone; an unreaped zombie is not enough.
			p, readErr := readLinuxProcess(pid)
			if errors.Is(readErr, os.ErrNotExist) || (readErr == nil && p.born != born) {
				continue
			}
			result = errors.Join(result, errors.New("owned descendant reaping unconfirmed"), err, readErr)
			continue
		}
		if err != nil {
			result = errors.Join(result, errors.New("owned descendant reaping unavailable"), err)
		}
	}
	return result
}
