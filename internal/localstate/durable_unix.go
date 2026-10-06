//go:build !windows

package localstate

import (
	"os"
	"path/filepath"
)

func renameStateFile(from, to string) error { return os.Rename(from, to) }

// SyncParent makes a successful rename durable on Unix filesystems.
func SyncParent(path string) error {
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
