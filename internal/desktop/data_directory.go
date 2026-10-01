package desktop

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
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
	return defaultApplicationDataDirectory()
}

func defaultApplicationDataDirectory() (string, error) {
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

// Select the same profile before optional shell recovery, so an explicitly
// paired thin APP never invokes local runtime shell discovery. Only the known
// historical product Notebook override is interpreted; custom HOME is retained.
func applicationDataDirectoryBeforeRuntimeEnvironment() (string, error) {
	if _, explicit := os.LookupEnv("CAELIS_BOT_DATA_DIR"); explicit {
		return applicationDataDirectory()
	}
	return defaultApplicationDataDirectoryBeforeRuntimeEnvironment()
}

func defaultApplicationDataDirectoryBeforeRuntimeEnvironment() (string, error) {
	root, err := defaultApplicationDataDirectory()
	if err != nil || runtime.GOOS != "darwin" {
		return root, err
	}
	inherited := filepath.Clean(os.Getenv("HOME"))
	for _, owned := range environmentNotebookHomes() {
		if inherited != owned {
			continue
		}
		account, err := user.Current()
		if err != nil {
			return "", err
		}
		name, _ := applicationIdentity()
		return filepath.Join(account.HomeDir, "Library", "Application Support", name), nil
	}
	return root, nil
}
