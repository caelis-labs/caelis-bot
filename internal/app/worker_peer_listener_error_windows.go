package app

import (
	"errors"

	"golang.org/x/sys/windows"
)

func peerListenerAddressInUse(err error) bool {
	// Go's syscall.EADDRINUSE is an invented APPLICATION_ERROR value on
	// Windows; bind returns the distinct Winsock WSAEADDRINUSE (10048).
	// WSAEACCES (10013) can also accompany an exclusive bind conflict, but
	// cannot distinguish that from an access restriction. Keep it unavailable.
	// https://learn.microsoft.com/en-us/windows/win32/winsock/so-exclusiveaddruse
	return errors.Is(err, windows.WSAEADDRINUSE)
}
