package plugins

import (
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

func TestWindowsOAuthLockFileIsPrivateAndCannotBeReplaced(t *testing.T) {
	root := t.TempDir()
	name := "oauth-private.lock"
	f, err := windowsLockFile(root, name, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	path := filepath.Join(root, name)
	if !localstate.IsPrivate(path) {
		t.Fatal("OAuth lock lost its private ACL")
	}
	if err := os.Rename(path, filepath.Join(root, "replaced")); err == nil {
		t.Fatal("open OAuth lock allowed delete sharing")
	}
}

func TestWindowsOAuthLockFileRejectsRedirectedObjects(t *testing.T) {
	for _, kind := range []string{"directory", "hardlink", "reparse"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "oauth-fixture.lock")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "hardlink", "reparse":
				target := filepath.Join(root, "target")
				if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "hardlink" {
					if err := os.Link(target, path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(target, path); err != nil {
					t.Skip("native symlink privilege unavailable")
				}
			}
			f, err := windowsLockFile(root, filepath.Base(path), true)
			if err == nil {
				f.Close()
				t.Fatal("OAuth lock accepted " + kind)
			}
		})
	}
}
