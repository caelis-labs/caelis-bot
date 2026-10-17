//go:build !windows

package runtimeenv

import "os/exec"

func BackgroundCommand(*exec.Cmd) {}
