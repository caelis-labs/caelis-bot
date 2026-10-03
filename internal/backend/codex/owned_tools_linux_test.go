package codex

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxHandleObservationRetriesInterruptedPoll(t *testing.T) {
	calls := 0
	exited, err := linuxHandleExitedWithPoll(42, func(fds []unix.PollFd, timeout int) (int, error) {
		calls++
		if len(fds) != 1 || fds[0].Fd != 42 || timeout != 0 {
			t.Fatal("poll lost the exact captured handle")
		}
		if calls < 3 {
			return 0, unix.EINTR
		}
		fds[0].Revents = unix.POLLIN
		return 1, nil
	})
	if err != nil || !exited || calls != 3 {
		t.Fatalf("interrupted observation: exited=%v calls=%d err=%v", exited, calls, err)
	}
}

func TestLinuxHandleObservationPreservesFailures(t *testing.T) {
	exited, err := linuxHandleExitedWithPoll(42, func([]unix.PollFd, int) (int, error) {
		return 0, unix.EACCES
	})
	if exited || !errors.Is(err, unix.EACCES) {
		t.Fatalf("observation failure hidden: exited=%v err=%v", exited, err)
	}
	exited, err = linuxHandleExitedWithPoll(42, func(fds []unix.PollFd, _ int) (int, error) {
		fds[0].Revents = unix.POLLNVAL
		return 1, nil
	})
	if exited || err == nil {
		t.Fatalf("invalid handle accepted: exited=%v err=%v", exited, err)
	}
}

func TestLinuxProcessIdentityParsing(t *testing.T) {
	stat := "42 (tool (worker) with\nspaces)) S 7 " + strings.Repeat("0 ", 17) + "12345 0 0"
	p, err := parseLinuxProcess([]byte(stat))
	if err != nil || p.pid != 42 || p.parent != 7 || p.born != 12345 || p.state != "S" {
		t.Fatalf("identity parse: %+v %v", p, err)
	}
	for _, bad := range []string{"", "42 missing comm", "42 (worker) S", strings.Replace(stat, "12345", "bad", 1)} {
		if _, err := parseLinuxProcess([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid process identity %q", bad)
		}
	}
}

func TestLinuxStaleRootDoesNotCaptureDescendants(t *testing.T) {
	o := newOwnedTools(os.Getpid())
	defer o.terminate()
	if o.failure() != nil {
		t.Fatal(o.failure())
	}
	o.born--
	o.capture()
	if len(o.children) != 0 {
		t.Fatal("reused root identity granted cleanup authority")
	}
}

func TestLinuxStableSignalPreservesChangedIdentity(t *testing.T) {
	p, err := readLinuxProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	// A mismatched identity must not receive any signal, even from a valid fd.
	fd, err := unix.PidfdOpen(p.pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := signalLinuxHandle(fd, p.pid, p.born+1, unix.SIGKILL); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxMissingIdentityReportsCleanupUncertainty(t *testing.T) {
	o := newOwnedTools(1 << 30)
	if o.failure() == nil {
		t.Fatal("missing identity silently claimed complete ownership")
	}
	o.capture()
	if o.failure() == nil {
		t.Fatal("unknown ownership was cleared")
	}
}

func TestLinuxUncapturedParentExitPreservesCleanupUncertainty(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	owner := newOwnedTools(child.Process.Pid)
	defer owner.terminate()
	if err := owner.failure(); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	owner.capture()
	if owner.failure() == nil {
		t.Fatal("parent exit before capture silently claimed complete cleanup")
	}
}

// procfs can report ESRCH when an enumerated process exits during the read.
// It proves a vanished identity, while permission failures remain uncertain.
func TestLinuxProcessReadClassifiesDisappearingIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		gone bool
	}{
		{"removed", os.ErrNotExist, true},
		{"exited during read", unix.ESRCH, true},
		{"permission denied", unix.EACCES, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readLinuxProcessWithReadFile(42, func(path string) ([]byte, error) {
				if path != "/proc/42/stat" {
					t.Fatalf("read changed original process target: %q", path)
				}
				return nil, &os.PathError{Op: "read", Path: path, Err: tc.err}
			})
			if errors.Is(err, os.ErrNotExist) != tc.gone || !errors.Is(err, tc.err) {
				t.Fatalf("process disappearance classification: gone=%v err=%v", errors.Is(err, os.ErrNotExist), err)
			}
		})
	}
}

func TestLinuxOwnedHandlesAreReleasedIdempotently(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	owner := newOwnedTools(child.Process.Pid)
	if err := owner.failure(); err != nil {
		t.Fatal(err)
	}
	fd := owner.rootHandle
	owner.releaseHandles()
	owner.releaseHandles()
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatal("stable handle leaked", err)
	}
	if owner.rootHandle != -1 || len(owner.handles) != 0 || !owner.closed {
		t.Fatal("cleanup was not final")
	}
}
