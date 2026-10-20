//go:build darwin || linux

package managedupdate

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestLeaseHelper(t *testing.T) {
	if os.Getenv("BOT_UPDATE_LEASE_HELPER") != "1" {
		return
	}
	fmt.Println("lease-held")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	os.Exit(0)
}

func TestInstallationLeaseOutlivesSupervisorDescriptor(t *testing.T) {
	_, f := fixtureManager(t)
	lease, e := acquireLease(f.root)
	if e != nil {
		t.Fatal(e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestLeaseHelper$")
	cmd.Env = append(os.Environ(), "BOT_UPDATE_LEASE_HELPER=1")
	cmd.ExtraFiles = []*os.File{lease}
	input, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	output, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { input.Close(); cmd.Wait() }()
	line, e := bufio.NewReader(output).ReadString('\n')
	if e != nil || line != "lease-held\n" {
		t.Fatal(line, e)
	}
	lease.Close()
	if unlock, e := lockDirectory(f.root); e == nil {
		unlock()
		t.Fatal("orphan Bot lost installation lease")
	}
}
