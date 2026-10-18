package plugins

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"golang.org/x/sys/windows"
)

const desktopOwnerFile = ".desktop-world-owner.lock"

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

func lockDesktopOwner(root string) (*os.File, error) {
	f, err := windowsLockFile(root, desktopOwnerFile, true)
	if err != nil {
		return nil, err
	}
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func desktopOwnerActive(root string) bool {
	f, err := windowsLockFile(root, desktopOwnerFile, false)
	if err != nil {
		return false
	}
	defer f.Close()
	overlapped := windows.Overlapped{}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err == nil {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
	}
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
