package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsDesktopOwnerDiesWithProcess(t *testing.T) {
	if root := os.Getenv("BOT_TEST_OWNER_ROOT"); root != "" {
		f, err := lockDesktopOwner(root)
		if err != nil {
			os.Exit(93)
		}
		defer f.Close()
		if err := os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600); err != nil {
			os.Exit(94)
		}
		time.Sleep(time.Minute)
		return
	}
	root := t.TempDir()
	if desktopOwnerActive(root) {
		t.Fatal("owner existed before launch")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestWindowsDesktopOwnerDiesWithProcess$")
	cmd.Env = append(os.Environ(), "BOT_TEST_OWNER_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not establish owner")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !desktopOwnerActive(root) {
		t.Fatal("native lock not visible across processes")
	}
	if f, err := lockDesktopOwner(root); err == nil {
		f.Close()
		t.Fatal("second owner admitted")
	}
	if err := os.Rename(filepath.Join(root, desktopOwnerFile), filepath.Join(root, "moved")); err == nil {
		t.Fatal("locked owner path could be replaced")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if desktopOwnerActive(root) {
		t.Fatal("stale process still authorizes desktop effects")
	}
	f, err := lockDesktopOwner(root)
	if err != nil {
		t.Fatal("new owner cannot recover", err)
	}
	f.Close()
}

func TestWindowsOAuthLockSerializesRefresh(t *testing.T) {
	root := t.TempDir()
	release, err := lockOAuth(root, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		unlock, err := lockOAuth(root, "fixture")
		if err == nil {
			unlock()
		}
		waiting <- err
	}()
	select {
	case <-waiting:
		release()
		t.Fatal("concurrent refresh admitted")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case err := <-waiting:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refresh lock not released")
	}
}
