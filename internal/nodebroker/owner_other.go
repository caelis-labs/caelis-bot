//go:build !darwin && !linux

package nodebroker

import "os"

func fileOwner(os.FileInfo) bool { return false }
