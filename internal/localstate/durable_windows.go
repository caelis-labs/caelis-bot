//go:build windows

package localstate

import "golang.org/x/sys/windows"

// MoveFileEx with WRITE_THROUGH is the Windows rename durability boundary.
// The temporary file has already been flushed and closed by Write.
func renameStateFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// SyncParent is part of the common care contract. Windows performs its
// write-through at the rename itself; directory File.Sync is unsupported.
func SyncParent(string) error { return nil }
