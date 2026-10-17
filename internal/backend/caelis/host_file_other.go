//go:build !windows

package caelis

func readHostFile(path string, limit int64) ([]byte, error) { return privateRead(path, limit) }
