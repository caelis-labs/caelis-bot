package chatlog

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// FileRenameInfoEx can replace the previous read-only projection atomically
// while preserving the new file's read-only attribute and open readers.
func replaceHistoryFile(from, to string) error {
	target, err := windows.FullPath(to)
	if err != nil {
		return err
	}
	name, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(source, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	type fileRenameInfo struct {
		flags  uint32
		root   windows.Handle
		length uint32
		name   [1]uint16
	}
	var header fileRenameInfo
	buf := make([]byte, max(int(unsafe.Sizeof(header)), int(unsafe.Offsetof(header.name))+2*len(name)))
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS | windows.FILE_RENAME_IGNORE_READONLY_ATTRIBUTE
	info.length = uint32(2 * (len(name) - 1))
	copy(unsafe.Slice(&info.name[0], len(name)), name)
	return windows.SetFileInformationByHandle(handle, windows.FileRenameInfoEx, &buf[0], uint32(len(buf)))
}
