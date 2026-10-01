//go:build !darwin && !linux

package main

import (
	"errors"
	"os/exec"
)

func detachNotebookOwner(*exec.Cmd) error {
	return errors.New("Notebook Bot ownership unsupported on this platform")
}
