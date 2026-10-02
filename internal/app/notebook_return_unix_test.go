//go:build darwin || linux

package app

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestNotebookReturnRefusesExistingLocalNativeOwner(t *testing.T) {
	c, _, _ := returnFixture(t)
	path := filepath.Join(c.app.root, ".product-owner.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if err := c.preserveLocalNotebook("fixture-bot", "original-op"); err == nil {
		t.Fatal("occupied local profile admitted")
	}
	if _, err := os.Stat(filepath.Join(c.app.root, "Notebook", "old-only.md")); err != nil {
		t.Fatal("old note changed", err)
	}
}
