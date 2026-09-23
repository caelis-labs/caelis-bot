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
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "Caelis Bot"), nil
}
