//go:build darwin

package desktop

import (
	"os"
	"os/user"
	"path/filepath"
)

func nativeDataDirectoryBase() (string, error) { return os.UserConfigDir() }

// Only product-owned Notebook paths are recognized as legacy HOME overrides.
func environmentNotebookHomes() []string {
	var paths []string
	if root := os.Getenv("CAELIS_BOT_DATA_DIR"); filepath.IsAbs(root) {
		paths = append(paths, filepath.Join(root, "Notebook"))
	}
	if u, err := user.Current(); err == nil {
		for _, app := range []string{"Caelis Bot", "Caelis Bot Dev", "Caelis Bot Dev Release"} {
			paths = append(paths, filepath.Join(u.HomeDir, "Library", "Application Support", app, "Notebook"))
		}
	}
	return paths
}
