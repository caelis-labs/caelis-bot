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
