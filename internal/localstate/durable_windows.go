//go:build windows

package localstate

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

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
	// Ordinary Windows readers (including os.ReadFile) need not share DELETE
	// access. Their short-lived handles must not turn a receipt poll into a lost
	// durable update. Retry only native sharing/access conflicts, with a budget
	// below Write's timeout. Never remove the original or fall back to truncation.
	deadline := time.Now().Add(time.Second)
	for {
		err = windows.MoveFileEx(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil || (!errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED)) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// SyncParent is part of the common care contract. Windows performs its
// write-through at the rename itself; directory File.Sync is unsupported.
func SyncParent(string) error { return nil }
