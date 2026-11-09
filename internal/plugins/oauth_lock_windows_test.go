package plugins

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
)

func TestWindowsOAuthContenderDoesNotChangeLockedACL(t *testing.T) {
	root := t.TempDir()
	release, err := lockOAuth(root, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			release()
		}
	}()
	path := filepath.Join(root, "oauth-fixture.lock")
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the same user's effective access, but deliberately use two ACEs.
	// This is not the canonical private ACL and must not be normalized by a
	// contender until the existing refresh owner releases its byte-range lock.
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid + ")(A;;FR;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if localstate.IsPrivate(path) {
		t.Fatal("two-ACE fixture unexpectedly passed canonical private attestation")
	}
	started, waiting := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		unlock, err := lockOAuth(root, "fixture")
		if err == nil {
			unlock()
		}
		waiting <- err
	}()
	<-started
	select {
	case err := <-waiting:
		t.Fatalf("contender returned before exclusive admission: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	changedWhileHeld := localstate.IsPrivate(path)
	release()
	held = false
	select {
	case err := <-waiting:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("contender did not finish after lock release")
	}
	if changedWhileHeld {
		t.Fatal("contender normalized the owner's ACL before exclusive admission")
	}
	if !localstate.IsPrivate(path) {
		t.Fatal("admitted contender did not establish and attest the private ACL")
	}
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
