//go:build darwin || linux

package app

import (
	"errors"
	"os"
	"syscall"
)

func lockNativeRoamingSupervisor(path string) (func(), error) {
	return lockNativeRoamingFile(path, true)
}
func lockNativeRoamingIntent(path string) (func(), error) { return lockNativeRoamingFile(path, false) }
func lockNativeRoamingFile(path string, nonblocking bool) (func(), error) {
	if s, e := os.Lstat(path); e == nil && (!s.Mode().IsRegular() || s.Mode().Perm() != 0600) {
		return nil, errors.New("native supervisor lock is not private")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	flags := syscall.LOCK_EX
	if nonblocking {
		flags |= syscall.LOCK_NB
	}
	if e = syscall.Flock(int(f.Fd()), flags); e != nil {
		f.Close()
		return nil, errors.New("native supervisor already owned")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
