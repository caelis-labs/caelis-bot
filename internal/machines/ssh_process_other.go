//go:build !darwin && !linux

package machines

import (
	"os/exec"
	"time"
)

// Unix SSH transport is not offered here; keep failed command drains bounded.
func configureSSHCommand(cmd *exec.Cmd) { cmd.WaitDelay = 250 * time.Millisecond }
