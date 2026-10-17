//go:build !windows

package testutil

import "os"

func Redirect(target, link string) error          { return os.Symlink(target, link) }
func SetMode(path string, mode os.FileMode) error { return os.Chmod(path, mode) }
