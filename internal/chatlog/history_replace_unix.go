//go:build !windows

package chatlog

import "os"

func replaceHistoryFile(from, to string) error { return os.Rename(from, to) }

func deleteHistoryFile(path string) error { return os.Remove(path) }
