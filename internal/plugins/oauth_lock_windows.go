//go:build windows

package plugins

import (
	"errors"
	"golang.org/x/sys/windows"
	"strings"
)

func lockOAuth(root, id string) (func(), error) {
	if id == "" || strings.ContainsAny(id, `/\:`) || id == "." || id == ".." {
		return nil, errors.New("invalid OAuth lock identity")
	}
	f, err := windowsLockFile(root, "oauth-"+id+".lock", true)
	if err != nil {
		return nil, err
	}
	overlapped := windows.Overlapped{}
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped); _ = f.Close() }, nil
}
