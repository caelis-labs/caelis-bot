package codex

import (
	"os"
	"testing"
)

func TestPrivateOwnerLifecycleChecksProcessBirthOnLinux(t *testing.T) {
	p, err := readLinuxProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	id := ownerProcessIdentity{PID: os.Getpid(), Birth: p.born}
	if got := privateOwnerLifecycle(id); got != ownerAlive {
		t.Fatal("current process was not identified as alive", got)
	}
	id.Birth++
	if got := privateOwnerLifecycle(id); got != ownerGone {
		t.Fatal("reused PID was mistaken for the original process", got)
	}
}
