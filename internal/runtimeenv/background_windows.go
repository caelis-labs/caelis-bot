package runtimeenv

import (
	"os/exec"
	"syscall"
)

// BackgroundCommand keeps a CLI management/probe operation off the desktop.
// The runtime's own service lifecycle still owns any detached shared Host.
func BackgroundCommand(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
