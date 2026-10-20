//go:build !windows

package caelisruntime

import (
	"os"
	"path/filepath"
)

// The executable's own bundle is the only embedded-runtime location. Never
// search another app, the working directory, or a writable configuration file.
func bundledPath(executable string) string {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	// Portable headless distributions retain the Helpers/Resources layout.
	// The marker distinguishes a damaged distribution from an ordinary CLI;
	// a missing helper must not silently fall back to an unrelated PATH binary.
	directory := filepath.Dir(resolved)
	if _, err := os.Lstat(filepath.Join(directory, "Resources", "Caelis-Runtime.json")); err == nil {
		return filepath.Join(directory, "Helpers", "caelis")
	}
	macos := directory
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return ""
	}
	return filepath.Join(contents, "Helpers", "caelis")
}
