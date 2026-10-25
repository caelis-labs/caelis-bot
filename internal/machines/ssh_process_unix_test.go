//go:build darwin || linux

package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSSHCancelledProxyReleasesPipesAndOwnedChildren(t *testing.T) {
	if _, err := os.Stat("/usr/bin/ssh"); err != nil {
		t.Skip("native SSH unavailable")
	}
	dir := t.TempDir()
	proxyPID, childPID := filepath.Join(dir, "proxy.pid"), filepath.Join(dir, "child.pid")
	script := filepath.Join(dir, "proxy.sh")
	body := fmt.Sprintf("#!/bin/sh\ntrap '' HUP\necho $$ > %s\n/bin/sleep 30 &\necho $! > %s\nwait\n", quote(proxyPID), quote(childPID))
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte("Host controlled-proxy\n HostName 192.0.2.1\n ProxyCommand /bin/sh "+quote(script)+"\n ControlMaster no\n ControlPath none\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd := sshCommand(ctx, "/usr/bin/ssh", "-F", config, "-T", "-o", "BatchMode=yes", "--", "controlled-proxy", "true")
	var diagnostic sshDiagnostic
	cmd.Stderr = &diagnostic
	done := make(chan error, 1)
	go func() { _, err := cmd.Output(); done <- err }()
	var pids []int
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	for _, file := range []string{proxyPID, childPID} {
		end := time.Now().Add(3 * time.Second)
		for {
			if b, err := os.ReadFile(file); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 1 {
					pids = append(pids, pid)
					break
				}
			}
			if time.Now().After(end) {
				t.Fatal("controlled proxy did not start")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled SSH reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("proxy retained Output pipes after cancellation")
	}
	for _, pid := range pids {
		end := time.Now().Add(time.Second)
		for {
			// A reaping zombie has exited; it cannot hold pipes or execute work.
			if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
				break
			} else if err != nil {
				t.Fatalf("cannot attest owned child exit: %v", err)
			}
			state, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
			if err != nil {
				if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					break
				}
				t.Fatalf("cannot attest owned child state: %v", err)
			}
			if strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
				break
			}
			if time.Now().After(end) {
				t.Fatalf("owned proxy child %d remained active", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
