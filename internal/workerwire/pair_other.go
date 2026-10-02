//go:build !darwin && !linux

package workerwire

import "os"

func pairFileOwner(os.FileInfo) bool { return false }
