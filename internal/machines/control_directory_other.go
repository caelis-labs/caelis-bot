//go:build !windows

package machines

func controlDirectoryRoot() string { return "/tmp" }
