//go:build darwin || linux

package app

import (
	"errors"
	"golang.org/x/sys/unix"
	"path/filepath"
)

func notebookProfileStopped(profile string) error {
	fd, err := unix.Open(filepath.Join(profile, ".product-owner.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("local profile owner lock unsafe")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("local profile has a native owner")
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return nil
}
