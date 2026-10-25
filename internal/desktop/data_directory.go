package desktop

import (
	"errors"
	"os"
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
	config, err := nativeDataDirectoryBase()
	if err != nil {
		return "", err
	}
	profile, err := developmentDefault(config)
	if err != nil {
		return "", err
	}
	if profile.DataDirectory != "" {
		return filepath.Clean(profile.DataDirectory), nil
	}
	name, _ := applicationIdentity()
	return filepath.Join(config, name), nil
}
