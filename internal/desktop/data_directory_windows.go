//go:build windows

package desktop

import (
	"errors"
	"os"
	"path/filepath"
)

// Product data is machine-local and survives package replacement. The Dev and
// release identities remain separate names under the same base directory.
func nativeDataDirectoryBase() (string, error) {
	root := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(root) {
		return "", errors.New("LOCALAPPDATA must be an absolute directory")
	}
	return root, nil
}

func environmentNotebookHomes() []string { return nil }
