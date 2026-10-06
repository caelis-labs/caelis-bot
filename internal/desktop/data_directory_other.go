//go:build !darwin && !windows

package desktop

import "os"

func nativeDataDirectoryBase() (string, error) { return os.UserConfigDir() }
func environmentNotebookHomes() []string       { return nil }
