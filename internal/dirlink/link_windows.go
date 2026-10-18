package dirlink

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// An NTFS directory junction preserves on-demand Skill reads and relative
// references without Windows' symlink privilege or a second copied inventory.
// Create only a new entry; removal of that entry never removes its target.
func Create(target, link string) (err error) {
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) || len(filepath.VolumeName(target)) != 2 || strings.Contains(target, "\x00") {
		return errors.New("Bot Skill junction requires an absolute local directory")
	}
	substitute, err := windows.UTF16FromString(`\??\` + target)
	if err != nil {
		return err
	}
	printName, err := windows.UTF16FromString(target)
	if err != nil {
		return err
	}
	// REPARSE_DATA_BUFFER: 8-byte header, 8-byte mount-point offsets,
	// then the two NUL-terminated UTF-16 names. Offsets exclude the header.
	size := 16 + 2*(len(substitute)+len(printName))
	if size > 16*1024 {
		return errors.New("Bot Skill junction path too long")
	}
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(size-8))
	binary.LittleEndian.PutUint16(data[10:], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(data[12:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(data[14:], uint16(2*(len(printName)-1)))
	for i, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+2*i:], value)
	}
	if err = os.Mkdir(link, 0700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(link)
		}
	}()
	name, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	var returned uint32
	return windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &data[0], uint32(len(data)), nil, 0, &returned, nil)
}
