package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Capture descendants while the exact launched parent is alive. /proc starttime
// is an identity, not a timestamp used to infer ownership. Only ancestry grants
// cleanup authority; process names, process groups and model-provided PIDs do not.
type ownedTools struct {
	mu         sync.Mutex
	root       int
	reaper     linuxProcess
	born       uint64
	children   map[int]uint64
	handles    map[int]int
	rootHandle int
	closed     bool
	captured   bool
	err        error
}

type linuxProcess struct {
	pid, parent int
	born        uint64
	state       string
}

func parseLinuxProcess(data []byte) (linuxProcess, error) {
	// comm may contain spaces, newlines and closing parentheses. The last ')'
	// ends comm; the remaining stat fields have no parentheses.
	s := string(data)
	open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 1 || close <= open {
		return linuxProcess{}, errors.New("invalid process identity")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	fields := strings.Fields(s[close+1:])
	if err != nil || pid < 1 || len(fields) < 20 {
		return linuxProcess{}, errors.New("invalid process identity")
	}
	parent, e1 := strconv.Atoi(fields[1])             // field 4
	born, e2 := strconv.ParseUint(fields[19], 10, 64) // field 22
	if e1 != nil || e2 != nil || parent < 0 || born == 0 || len(fields[0]) != 1 {
		return linuxProcess{}, errors.New("invalid process identity")
	}
	return linuxProcess{pid, parent, born, fields[0]}, nil
}

func readLinuxProcess(pid int) (linuxProcess, error) {
	return readLinuxProcessWithReadFile(pid, os.ReadFile)
}

func readLinuxProcessWithReadFile(pid int, readFile func(string) ([]byte, error)) (linuxProcess, error) {
	data, err := readFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	// procfs may return ESRCH after enumeration when the process exits during
	// this read. Classify that vanished identity as missing, retaining the native
	// cause; permission and other observation failures must remain uncertain.
	if errors.Is(err, unix.ESRCH) {
		err = errors.Join(os.ErrNotExist, err)
	}
	if err != nil {
		return linuxProcess{}, err
	}
	p, err := parseLinuxProcess(data)
	if err == nil && p.pid != pid {
		err = errors.New("process identity changed")
	}
	return p, err
}

func newOwnedTools(pid int) *ownedTools {
	o := &ownedTools{root: pid, children: map[int]uint64{}, handles: map[int]int{}, rootHandle: -1}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		o.err = errors.New("owned process stable handle unavailable")
		return o
	}
	o.rootHandle = fd
	p, err := readLinuxProcess(pid)
	if err != nil {
		o.err = errors.New("owned process identity unavailable")
	} else {
		o.born = p.born
	}
	return o
}

func (o *ownedTools) capture() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	if o.rootHandle < 0 {
		o.err = errors.New("owned process stable handle unavailable")
		return
	}
	ready, err := linuxHandleExited(o.rootHandle)
	if err != nil {
		o.err = err
		return
	}
	if ready && o.reaper.pid == 0 {
		if !o.captured {
			o.err = errors.New("owned descendant discovery unavailable after parent exit")
		}
		return
	}
	root, err := readLinuxProcess(o.root)
	if errors.Is(err, os.ErrNotExist) && o.reaper.pid != 0 {
		root = linuxProcess{pid: o.root, born: o.born}
		err = nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil || o.born == 0 {
		o.err = errors.New("owned process identity unavailable")
		return
	}
	if root.born != o.born {
		if o.reaper.pid == 0 {
			return
		}
		root = linuxProcess{pid: o.root, born: o.born}
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		o.err = errors.New("owned descendant discovery unavailable")
		return
	}
	processes := map[int]linuxProcess{o.root: root}
	if o.reaper.pid != 0 {
		current, err := readLinuxProcess(o.reaper.pid)
		if err != nil || current.born != o.reaper.born {
			o.err = errors.New("owned watchdog identity unavailable")
			return
		}
		processes[current.pid] = current
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || pid == o.root {
			continue
		}
		p, err := readLinuxProcess(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			// A restricted or unreadable process table cannot prove full cleanup.
			o.err = errors.New("owned descendant discovery incomplete")
			continue
		}
		processes[pid] = p
	}
	owned := map[int]bool{o.root: true}
	if o.reaper.pid != 0 {
		owned[o.reaper.pid] = true
	}
	for changed := true; changed; {
		changed = false
		for pid, p := range processes {
			if owned[pid] || !owned[p.parent] {
				continue
			}
			// Recheck both ends of an ancestry edge before retaining authority.
			parent, e1 := readLinuxProcess(p.parent)
			child, e2 := readLinuxProcess(pid)
			if e1 != nil || e2 != nil || parent.born != processes[p.parent].born || child.born != p.born || child.parent != p.parent {
				continue
			}
			fd, err := unix.PidfdOpen(pid, 0)
			if errors.Is(err, unix.ESRCH) {
				continue
			}
			if err != nil {
				o.err = errors.New("owned descendant stable handle unavailable")
				continue
			}
			confirmed, err := readLinuxProcess(pid)
			if err != nil || confirmed.born != p.born || confirmed.parent != p.parent {
				_ = unix.Close(fd)
				continue
			}
			// Keep the captured handle through reparenting and PID reuse.
			if previous, ok := o.handles[pid]; ok {
				_ = unix.Close(previous)
			}
			o.handles[pid] = fd
			owned[pid] = true
			o.children[pid] = p.born
			changed = true
		}
	}
	o.captured = true
}

func sameLiveProcess(pid int, born uint64) bool {
	p, err := readLinuxProcess(pid)
	return err == nil && p.born == born && p.state != "Z" && p.state != "X"
}

// Captured handles bind cleanup to the original process. A reused numeric PID
// never receives a signal, even when /proc starttime has coarse clock precision.
func signalLinuxHandle(fd, pid int, born uint64, signal unix.Signal) error {
	p, err := readLinuxProcess(pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("owned descendant identity unavailable")
	}
	if p.born != born || p.state == "Z" || p.state == "X" {
		return nil
	}
	err = unix.PidfdSendSignal(fd, signal, nil, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func (o *ownedTools) terminate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	defer o.releaseHandlesLocked()

	for pid, born := range o.children {
		fd, ok := o.handles[pid]
		if !ok {
			o.err = errors.New("owned descendant stable handle unavailable")
			continue
		}
		if err := signalLinuxHandle(fd, pid, born, unix.SIGTERM); err != nil {
			o.err = err
		}
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		live := false
		for pid, born := range o.children {
			fd, ok := o.handles[pid]
			if !ok {
				continue
			}
			running, err := linuxOwnedHandleLive(fd, pid, born)
			if err != nil {
				o.err = err
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
				fd, ok := o.handles[pid]
				if !ok {
					o.err = errors.New("owned descendant stable handle unavailable")
					continue
				}
				if err := signalLinuxHandle(fd, pid, born, unix.SIGKILL); err != nil {
					o.err = err
				}
			}
			// SIGKILL is dispatched, not proof of exit. Wait for bounded confirmation.
			for range 100 {
				live = false
				for pid, born := range o.children {
					fd, ok := o.handles[pid]
					if !ok {
						continue
					}
					running, err := linuxOwnedHandleLive(fd, pid, born)
					if err != nil {
						o.err = err
					}
					live = live || running
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

// pidfd readiness confirms the captured process exited, including an unreaped
// zombie. It never substitutes a newly created process with the same numeric PID.
func linuxHandleExited(fd int) (bool, error) {
	return linuxHandleExitedWithPoll(fd, unix.Poll)
}

func linuxHandleExitedWithPoll(fd int, poll func([]unix.PollFd, int) (int, error)) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		_, err := poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			// A signal (including Go async preemption) interrupts observation,
			// not the captured process. Retry without losing cleanup authority.
			continue
		}
		if err != nil {
			return false, fmt.Errorf("owned process exit observation unavailable: %w", err)
		}
		break
	}
	if fds[0].Revents&unix.POLLNVAL != 0 {
		return false, errors.New("owned process handle invalid")
	}
	return fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, nil
}

func linuxOwnedHandleLive(fd, pid int, born uint64) (bool, error) {
	exited, err := linuxHandleExited(fd)
	if err != nil || exited {
		return false, err
	}
	p, err := readLinuxProcess(pid)
	if err != nil {
		return false, errors.New("owned descendant cleanup unconfirmed")
	}
	return p.born == born && p.state != "Z" && p.state != "X", nil
}

// releaseHandles does not signal a process. Stop owns the final exit proof.
func (o *ownedTools) releaseHandles() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.releaseHandlesLocked()
}
func (o *ownedTools) releaseHandlesLocked() {
	if o.rootHandle >= 0 {
		_ = unix.Close(o.rootHandle)
		o.rootHandle = -1
	}
	for pid, fd := range o.handles {
		_ = unix.Close(fd)
		delete(o.handles, pid)
	}
}
