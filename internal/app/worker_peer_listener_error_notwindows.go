//go:build !windows

package app

import (
	"errors"
	"syscall"
)

func peerListenerAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
