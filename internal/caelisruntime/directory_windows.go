package caelisruntime

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// A detached Host inherits its launcher CWD even after Core stages the EXE in
// AppData. Never let it retain a removable ZIP/Setup directory. This process
// directory is independent of each Session's Notebook/workspace configuration.
func prepareCommandDirectory(cmd *exec.Cmd) error {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return errors.New("runtime home directory unavailable")
	}
	cmd.Dir = home
	return nil
}
