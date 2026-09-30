//go:build linux || darwin

package nodeagent

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockAgent(directory string) (func(), error) {
	path := filepath.Join(directory, ".agent-owner.lock")
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm() != 0600) {
		return nil, errors.New("agent owner lock is not private")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, errors.New("agent directory already has a foreground owner")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
