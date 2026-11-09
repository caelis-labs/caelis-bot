//go:build windows

package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
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
	return func() { _ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped); _ = f.Close() }, nil
}

// Returns a locked, freshly attested handle; closing it also releases the lock.
// No delete sharing: a second process cannot replace the locked pathname and
// impersonate a fresh owner. OPEN_REPARSE_POINT refuses redirected lock files.
func windowsLockFile(root, name string, create bool) (*os.File, error) {
	path := filepath.Join(root, name)
	if !filepath.IsAbs(root) || filepath.Base(name) != name {
		return nil, errors.New("invalid lock path")
	}
	if create {
		if err := localstate.EnsurePrivateDirectory(root); err != nil {
			return nil, err
		}
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	creation := uint32(windows.OPEN_EXISTING)
	if create {
		creation = windows.OPEN_ALWAYS
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, creation, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(h, &info)
	if err == nil && (info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 || info.NumberOfLinks != 1) {
		err = errors.New("invalid lock object")
	}
	// ACL writers and readers must share the refresh lock. GetSecurityInfo
	// can fail when another caller changes the descriptor concurrently.
	if err == nil {
		overlapped := windows.Overlapped{}
		err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
	}
	if err == nil && create {
		err = localstate.ProtectFile(path)
	}
	if err == nil && !localstate.IsPrivate(path) {
		err = errors.New("lock is not private")
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
