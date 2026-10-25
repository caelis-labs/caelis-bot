package sshtransport

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

// ssh.exe owns the foreground channel; no shared control master is terminated.
func bindProcess(cmd *exec.Cmd) func() {
	cmd.WaitDelay = 2 * time.Second
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

func openConfig(path string) (*os.File, error) {
	v, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if !v.Mode().IsRegular() {
		return nil, errors.New("ssh_config_unreadable")
	}
	return os.Open(path)
}
