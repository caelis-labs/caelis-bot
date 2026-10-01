//go:build darwin || linux

package main

import (
	"os/exec"
	"syscall"
)

func detachNotebookOwner(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
