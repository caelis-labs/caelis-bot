package codex

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

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
