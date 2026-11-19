//go:build !windows

package botworkspace

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func tryConfigLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	return errors.Is(err, unix.EWOULDBLOCK), err
}
