//go:build darwin || linux

package machines

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureSSHCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	// Even a custom proxy that deliberately detaches cannot retain our pipes.
	cmd.WaitDelay = 250 * time.Millisecond
}
