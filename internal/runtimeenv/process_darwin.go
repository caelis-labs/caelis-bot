package runtimeenv

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"
	"time"
)

func userShell(ctx context.Context) string {
	if u, err := user.Current(); err == nil {
		cmd := exec.CommandContext(ctx, "/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell")
		if b, err := cmd.Output(); err == nil {
			if _, shell, ok := strings.Cut(string(b), "UserShell:"); ok {
				return strings.TrimSpace(shell)
			}
		}
	}
	return "/bin/zsh"
}

func boundProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if err == syscall.ESRCH {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 250 * time.Millisecond
}
