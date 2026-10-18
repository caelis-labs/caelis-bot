//go:build !windows

package dirlink

import "os"

func Create(target, link string) error { return os.Symlink(target, link) }
