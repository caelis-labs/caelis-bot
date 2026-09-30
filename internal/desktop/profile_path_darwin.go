//go:build darwin

package desktop

import (
	"bytes"
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// F_GETPATH preserves the existing directory's actual casing on case-insensitive
// volumes. Only directory metadata is read; no profile or auth file is opened.
func canonicalExistingProfilePath(path string) (string, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var buffer [1024]byte // Darwin PATH_MAX, as required by F_GETPATH.
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(buffer[:], 0)
	if end < 0 {
		return "", errors.New("native profile path exceeds its bound")
	}
	return string(buffer[:end]), nil
}
