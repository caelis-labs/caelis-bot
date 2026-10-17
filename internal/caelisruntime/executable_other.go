//go:build !windows

package caelisruntime

import "os"

func executableFile(_ string, info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&0111 != 0
}
