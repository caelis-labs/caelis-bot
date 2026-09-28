package desktop

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
)

// An explicit profile lets native acceptance use an isolated backend without
// moving or rewriting the user's everyday conversation and runtime settings.
func applicationDataDirectory() (string, error) {
	if root, set := os.LookupEnv("CAELIS_BOT_DATA_DIR"); set {
		if !filepath.IsAbs(root) {
			return "", errors.New("CAELIS_BOT_DATA_DIR must be an absolute directory")
		}
		return filepath.Clean(root), nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name, _ := applicationIdentity()
	return filepath.Join(config, name), nil
}

// Only paths owned by this product are recognized as legacy HOME overrides.
// A deliberate custom HOME or ZDOTDIR outside them remains the user's choice.
func environmentNotebookHomes() []string {
	var paths []string
	if root := os.Getenv("CAELIS_BOT_DATA_DIR"); filepath.IsAbs(root) {
		paths = append(paths, filepath.Join(root, "Notebook"))
	}
	if u, err := user.Current(); err == nil {
		for _, app := range []string{"Caelis Bot", "Caelis Bot Dev"} {
			paths = append(paths, filepath.Join(u.HomeDir, "Library", "Application Support", app, "Notebook"))
		}
	}
	return paths
}
