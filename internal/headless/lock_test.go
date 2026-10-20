//go:build !windows

package headless

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOneOwnerAndNoSymlinkLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "headless.lock")
	release, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lock(path); err == nil {
		second()
		release()
		t.Fatal("second owner admitted")
	}
	release()
	again, err := lock(path)
	if err != nil {
		t.Fatal(err)
	}
	again()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if release, err := lock(link); err == nil {
		release()
		t.Fatal("symlink accepted")
	}
	body, _ := os.ReadFile(target)
	if string(body) != "unchanged" {
		t.Fatal("lock followed symlink")
	}
}
