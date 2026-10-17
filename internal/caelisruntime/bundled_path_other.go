//go:build !windows

package caelisruntime

import "path/filepath"

// The executable's own bundle is the only embedded-runtime location. Never
// search another app, the working directory, or a writable configuration file.
func bundledPath(executable string) string {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	macos := filepath.Dir(resolved)
	contents := filepath.Dir(macos)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(filepath.Dir(contents)) != ".app" {
		return ""
	}
	return filepath.Join(contents, "Helpers", "caelis")
}
