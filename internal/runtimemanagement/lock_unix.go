//go:build darwin || linux

package runtimemanagement

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

func lockRoot(ctx context.Context, root *os.Root) (func(), error) {
	return lockRootUsing(ctx, root, syscall.Flock)
}

func lockRootUsing(ctx context.Context, root *os.Root, flock func(int, int) error) (func(), error) {
	file, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("runtime management lock unavailable")
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err = flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == syscall.EINTR {
			continue
		}
		if err == nil {
			return func() { _ = flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = file.Close()
			return nil, errors.New("runtime management lock unavailable")
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func privateOwner(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}
