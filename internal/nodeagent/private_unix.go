//go:build darwin || linux

package nodeagent

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// CheckPrivateDirectory never repairs permissions or adopts a shared directory.
func CheckPrivateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("explicit absolute private directory required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("private directory unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("directory must be private and owned by this user")
	}
	return nil
}

// CaptureJoinSocket verifies the newly created forwarding socket and returns
// cleanup that removes only that same socket identity after SSH detaches.
func CaptureJoinSocket(directory string) (func(), error) {
	if err := CheckPrivateDirectory(directory); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "agent.sock")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("private forwarded socket unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		return nil, errors.New("forwarded socket must be private to its SSH user")
	}
	return func() {
		current, err := os.Lstat(path)
		if err == nil && os.SameFile(info, current) {
			_ = os.Remove(path)
		}
	}, nil
}
