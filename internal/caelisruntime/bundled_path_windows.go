package caelisruntime

import "path/filepath"

// Set by the Windows application build, never by user environment or Store.
// Console tools and source tests keep normal external CLI discovery.
var windowsBundled = "0"

func bundledPath(executable string) string {
	if windowsBundled != "1" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(resolved), "Helpers", "caelis.exe")
}
