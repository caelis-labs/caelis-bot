//go:build !windows

package chatlog

import "os"

func replaceHistoryFile(from, to string) error { return os.Rename(from, to) }
