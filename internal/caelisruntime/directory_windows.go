package caelisruntime

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// A detached Host inherits its launcher CWD even after Core stages the EXE in
// AppData. Never let it retain a removable ZIP/Setup directory. This process
// directory is independent of each Session's Notebook/workspace configuration.
func prepareCommandDirectory(cmd *exec.Cmd) error {
	cache, err := os.UserCacheDir()
	if err != nil || !filepath.IsAbs(cache) {
		return errors.New("runtime local data directory unavailable")
	}
	// Core also uses this as its default workspace for clients that omit one.
	// Keep it separate from the user's home, Bot Notebook and Worker workspaces.
	dir := filepath.Join(cache, "Caelis Bot", "host-workspace")
	if err := localstate.EnsurePrivateDirectory(dir); err != nil {
		return err
	}
	cmd.Dir = dir
	return nil
}
