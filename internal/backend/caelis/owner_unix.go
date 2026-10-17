//go:build !windows

package caelis

import (
	"os"
	"syscall"
)

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func privateState(_ string, info os.FileInfo) bool {
	return info.Mode().Perm()&0077 == 0 && owned(info)
}
