//go:build darwin || linux

package runtimeinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This test-only child deliberately owns a different process group. Production
// cancellation must not broaden its kill scope to deal with inherited pipe FDs.
func TestCodexInstallerDetachedOutputHelper(t *testing.T) {
	if os.Getenv("RUNTIMEINSTALL_PIPE_HOLDER") != "1" {
		t.Skip("subprocess fixture only")
	}
	if err := syscall.Setpgid(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("INSTALL_TEST_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if _, err := os.Stat(os.Getenv("INSTALL_TEST_RELEASE")); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("fixture release not received")
}

func TestCodexInstallerCancellationClosesInheritedOutput(t *testing.T) {
	t.Run("both", func(t *testing.T) { testInheritedOutputCancellation(t, false) })
	t.Run("stderr-after-exit", func(t *testing.T) { testInheritedOutputCancellation(t, true) })
}

func testInheritedOutputCancellation(t *testing.T, stderrOnly bool) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	ready, release := filepath.Join(root, "ready"), filepath.Join(root, "release")
	effect := filepath.Join(root, "installer-survived")
	t.Setenv("RUNTIMEINSTALL_PIPE_HOLDER", "1")
	t.Setenv("INSTALL_TEST_READY", ready)
	t.Setenv("INSTALL_TEST_RELEASE", release)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	script := filepath.Join(root, "installer.sh")
	body := quote(executable) + " -test.run='^TestCodexInstallerDetachedOutputHelper$' &\nwait\nprintf survived > " + quote(effect) + "\n"
	parent := filepath.Join(root, "parent-pid")
	if stderrOnly {
		body = "printf '%s' \"$$\" > " + quote(parent) + "\n" + quote(executable) + " -test.run='^TestCodexInstallerDetachedOutputHelper$' > /dev/null &\nexit 0\n"
	}
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runCodexInstaller(ctx, script); close(done) }()
	t.Cleanup(func() {
		// Release only this fixture; never kill an unrelated process/group.
		_ = os.WriteFile(release, []byte("release"), 0600)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned installer fixture did not finish cleanup")
		}
	})
	until := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("detached output holder did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if stderrOnly {
		data, err := os.ReadFile(parent)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil || pid <= 1 {
			t.Fatal("invalid owned installer PID", string(data), err)
		}
		// ESRCH proves Wait reaped the installer. Its context watcher can no
		// longer be relied upon to close the remaining stderr reader.
		for syscall.Kill(pid, 0) != syscall.ESRCH {
			if time.Now().After(until) {
				t.Fatal("owned installer was not reaped before cancellation")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "取消") {
			t.Fatal("original cancellation result lost", err)
		}
	case <-time.After(700 * time.Millisecond):
		t.Fatal("inherited output blocked installer cancellation")
	}
	if _, err := os.Stat(effect); !os.IsNotExist(err) {
		t.Fatal("cancelled installer produced an effect", err)
	}
}

func TestCodexInstallerDrainsNormalErrorAfterStdoutCloses(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	script := filepath.Join(root, "installer.sh")
	// Close stdout before producing the bounded diagnostic tail. Waiting for
	// the process must not close the stderr reader before the copy completes.
	body := "exec 1>&-\nprintf '%016000d' 0 >&2\nprintf 'checksum mismatch\\n' >&2\nexit 23\n"
	if err := os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	err := runCodexInstaller(t.Context(), script)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "archive_invalid" || failure.ExitCode != 23 {
		t.Fatal("normal exit diagnostic was lost", err)
	}
}
