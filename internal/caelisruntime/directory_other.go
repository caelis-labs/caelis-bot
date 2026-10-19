//go:build !windows

package caelisruntime

import "os/exec"

func prepareCommandDirectory(*exec.Cmd) error { return nil }
