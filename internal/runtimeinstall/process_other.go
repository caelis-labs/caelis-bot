//go:build !darwin

package runtimeinstall

import "os/exec"

// Automatic installation is unavailable on these platforms.
func configureInstallerCommand(cmd *exec.Cmd) {}
