//go:build darwin || linux

package nodebroker

import (
	"os"
	"syscall"
)

func fileOwner(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Getuid())
}
