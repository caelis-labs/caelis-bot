//go:build !darwin

package runtimeenv

import (
	"context"
	"os/exec"
	"time"
)

// Compilation boundary only; desktop environment restoration ships on macOS.
func userShell(context.Context) string { return "" }
func boundProcess(cmd *exec.Cmd)       { cmd.WaitDelay = 250 * time.Millisecond }
