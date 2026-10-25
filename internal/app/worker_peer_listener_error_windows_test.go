package app

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWorkerListenerWindowsBindErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		used bool
	}{
		{"winsock address in use", windows.WSAEADDRINUSE, true},
		{"wrapped native bind", &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", windows.WSAEADDRINUSE)}, true},
		{"exclusive or restricted address", windows.WSAEACCES, false},
		{"wrapped exclusive or restricted address", &net.OpError{Op: "listen", Net: "tcp", Err: os.NewSyscallError("bind", windows.WSAEACCES)}, false},
		{"file access denied", windows.ERROR_ACCESS_DENIED, false},
		{"invented Go errno", syscall.EADDRINUSE, false},
		{"untrusted error text", errors.New("address already in use"), false},
		{"no error", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := peerListenerAddressInUse(tc.err); got != tc.used {
				t.Fatal("incorrect bind classification", got, tc.err)
			}
		})
	}
}

func TestWorkerListenerWindowsExclusiveBindDoesNotProvePermissionCause(t *testing.T) {
	// Real ephemeral socket fixture for the Windows-only exclusive-use behavior.
	// SO_EXCLUSIVEADDRUSE is defined by Winsock2.h as ~SO_REUSEADDR.
	// https://learn.microsoft.com/en-us/windows/win32/winsock/so-exclusiveaddruse
	const soExclusiveAddrUse = ^windows.SO_REUSEADDR
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var optionErr error
		if err := c.Control(func(fd uintptr) {
			optionErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
		}); err != nil {
			return err
		}
		return optionErr
	}}
	occupied, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("prepare exclusive-use fixture", err)
	}
	defer occupied.Close()
	second, err := net.Listen("tcp4", occupied.Addr().String())
	if second != nil {
		second.Close()
		t.Fatal("exclusive-use address accepted a second listener")
	}
	if !errors.Is(err, windows.WSAEADDRINUSE) && !errors.Is(err, windows.WSAEACCES) {
		t.Fatal("unexpected exclusive-use bind error", err)
	}
	if got := peerListenerAddressInUse(err); got != errors.Is(err, windows.WSAEADDRINUSE) {
		t.Fatal("ambiguous permission error was called an in-use address", err)
	}
	t.Log("native exclusive-use bind:", err)
}
