package desktop

import (
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/windowsupdate"
)

// Only an explicitly owned Setup installation integrates with the Windows
// shell. ZIP, source builds and damaged/unknown packages remain registration-free.
func windowsShellIntegration(executable, identity string) bool {
	// Even an unreadable/malformed ZIP marker must not fall through to stale
	// Setup metadata and create persistent system entries.
	if _, err := os.Lstat(filepath.Join(filepath.Dir(executable), "portable.json")); !os.IsNotExist(err) {
		return false
	}
	format, err := windowsupdate.Installation(filepath.Dir(executable), identity)
	return err == nil && format == "nsis"
}

func currentWindowsShellIntegration() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	_, identity := applicationIdentity()
	return windowsShellIntegration(executable, identity)
}
