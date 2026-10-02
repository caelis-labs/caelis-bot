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

// Only the dedicated watchdog can claim its adopted children. Ordinary APP
// process tracking never gains authority over unrelated children of the APP.
func newWatchdogTools(pid int) *ownedTools {
	o := newOwnedTools(pid)
	reaper, err := readLinuxProcess(os.Getpid())
	if err != nil {
		o.err = errors.New("owned watchdog identity unavailable")
	} else {
		o.reaper = reaper
	}
	return o
}

// After the native root Cmd has been waited, capture any final adoption by
// this dedicated watchdog and reap only exited stable handles. Mere numeric
// PIDs do not grant authority over unrelated processes.
func (o *ownedTools) reapWatchdogChildren() error {
	if o.reaper.pid != 0 {
		o.capture()
	} // include adoption after the native root exits
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
	if o.reaper.pid != 0 {
		result = errors.Join(result, confirmWatchdogChildrenReaped())
	}
	return result
}

// /proc enumeration can race one last adoption. Only ECHILD from this dedicated
// subreaper proves no executing or unreaped child remains. WNOWAIT observes
// without claiming an uncaptured exit status; WALL includes clone children.
func confirmWatchdogChildrenReaped() error {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_ALL, 0, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT|unix.WALL, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.ECHILD) {
			return nil
		}
		return errors.Join(errors.New("owned watchdog child reaping unconfirmed"), err)
	}
}
