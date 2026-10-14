package codex

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateOwnerLifecycleChecksProcessBirthOnDarwin(t *testing.T) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	id := ownerProcessIdentity{PID: os.Getpid(), Birth: darwinProcessBirth(p.Proc.P_starttime)}
	if got := privateOwnerLifecycle(id); got != ownerAlive {
		t.Fatal("current process was not identified as alive", got)
	}
	id.Birth++
	if got := privateOwnerLifecycle(id); got != ownerGone {
		t.Fatal("reused PID was mistaken for the original process", got)
	}
}
