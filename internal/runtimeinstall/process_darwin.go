package runtimeinstall

import (
	"os/exec"
	"syscall"
	"time"
)

// Only the explicitly requested installer owns this process group. Never apply
// this cancellation policy to a shared Codex or Caelis runtime.
func configureInstallerCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
}
