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
func ownedDarwinProcessLive(pid int, born unix.Timeval) (bool, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, syscall.ESRCH) || (errors.Is(err, syscall.EIO) && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("owned descendant exit observation unavailable")
	}
	return p.Proc.P_starttime == born && p.Proc.P_stat != 5, nil
}
func sameLiveProcess(pid int, born unix.Timeval) bool {
	live, err := ownedDarwinProcessLive(pid, born)
	return err == nil && live
}
func (o *ownedTools) terminate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for pid, born := range o.children {
		live, err := ownedDarwinProcessLive(pid, born)
		if err != nil {
			o.err = err
			continue
		}
		if live {
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
			running, err := ownedDarwinProcessLive(pid, born)
			if err != nil {
				o.err = err
				running = true
			}
			live = live || running
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
			// SIGKILL dispatch is not exit proof. Recheck the captured birth
			// identities and fail closed if observation or termination is uncertain.
			for range 100 {
				live = false
				for pid, born := range o.children {
					p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
					if errors.Is(err, syscall.ESRCH) || (errors.Is(err, syscall.EIO) && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)) {
						continue
					}
					if err != nil {
						o.err = errors.New("owned descendant exit observation unavailable")
						live = true
						continue
					}
					live = live || (p.Proc.P_starttime == born && p.Proc.P_stat != 5)
				}
				if !live {
					return
				}
				<-ticker.C
			}
			o.err = errors.New("owned descendant cleanup unconfirmed")
			return
		}
	}
}
func (o *ownedTools) failure() error { o.mu.Lock(); defer o.mu.Unlock(); return o.err }

func (o *ownedTools) releaseHandles() {}
