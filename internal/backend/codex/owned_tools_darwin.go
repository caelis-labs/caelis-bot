package codex

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Native interruption can drop a foreground tool before it appears in Codex's
// background-terminal registry. Record OS descendants while the owned parent is
// still alive, before interrupt/EOF can reparent them. Never discover by name or
// accept a model-provided PID. Birth times guard against reused process IDs.
type ownedTools struct {
	mu       sync.Mutex
	root     int
	born     unix.Timeval
	children map[int]unix.Timeval
	err      error
}

func newOwnedTools(pid int) *ownedTools {
	o := &ownedTools{root: pid, children: map[int]unix.Timeval{}}
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		o.err = err
	} else {
		o.born = p.Proc.P_starttime
	}
	return o
}
func (o *ownedTools) capture() {
	o.mu.Lock()
	defer o.mu.Unlock()
	root, err := unix.SysctlKinfoProc("kern.proc.pid", o.root)
	if errors.Is(err, syscall.ESRCH) || (errors.Is(err, syscall.EIO) && errors.Is(syscall.Kill(o.root, 0), syscall.ESRCH)) {
		return
	}
	if err != nil {
		o.err = err
		return
	}
	if root.Proc.P_starttime != o.born {
		return
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
	if err != nil {
		o.err = err
		return
	}
	owned := map[int]bool{o.root: true}
	for changed := true; changed; {
		changed = false
		for _, p := range processes {
			pid := int(p.Proc.P_pid)
			if pid > 1 && !owned[pid] && owned[int(p.Eproc.Ppid)] {
				owned[pid] = true
				o.children[pid] = p.Proc.P_starttime
				changed = true
			}
		}
	}
}
func sameLiveProcess(pid int, born unix.Timeval) bool {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && p.Proc.P_starttime == born && p.Proc.P_stat != 5 // SZOMB is already exited.
}
func (o *ownedTools) terminate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for pid, born := range o.children {
		if sameLiveProcess(pid, born) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				o.err = err
			}
		}
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		live := false
		for pid, born := range o.children {
			if sameLiveProcess(pid, born) {
				live = true
				break
			}
		}
		if !live {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			for pid, born := range o.children {
				if sameLiveProcess(pid, born) {
					if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
						o.err = err
					}
				}
			}
			return
		}
	}
}
func (o *ownedTools) failure() error { o.mu.Lock(); defer o.mu.Unlock(); return o.err }
