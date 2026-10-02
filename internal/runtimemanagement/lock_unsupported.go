//go:build !darwin && !linux

package runtimemanagement

import (
	"context"
	"errors"
	"os"
)

func lockRoot(context.Context, *os.Root) (func(), error) {
	return nil, errors.New("runtime installation unsupported")
}
func privateOwner(os.FileInfo) bool { return false }
