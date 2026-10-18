//go:build !windows

package codex

func installedWindowsBinary(path string) string { return path }
