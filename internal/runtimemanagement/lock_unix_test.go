//go:build darwin || linux

package runtimemanagement

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestManagementLockRetriesInterruptedSyscallOnly(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	calls := 0
	released := false
	unlock, err := lockRootUsing(context.Background(), root, func(_ int, operation int) error {
		if operation == syscall.LOCK_UN {
			released = true
			return nil
		}
		calls++
		if calls < 3 {
			return syscall.EINTR
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatal("interrupted lock did not retry", calls, err)
	}
	unlock()
	if !released {
		t.Fatal("successful lock did not release")
	}
	calls = 0
	if _, err := lockRootUsing(context.Background(), root, func(int, int) error { calls++; return syscall.EIO }); err == nil || calls != 1 {
		t.Fatal("real lock error was suppressed", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	if _, err := lockRootUsing(ctx, root, func(int, int) error { calls++; cancel(); return syscall.EINTR }); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("interrupted loop ignored cancellation", calls, err)
	}
}
