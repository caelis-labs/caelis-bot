//go:build !windows

package localstate

import "os"

// Preserve the existing POSIX creation and permission contracts.
func EnsurePrivateDirectory(path string) error { return os.MkdirAll(path, 0700) }
func ProtectDirectory(path string) error       { return os.Chmod(path, 0700) }
func ProtectFile(path string) error            { return os.Chmod(path, 0600) }
func ProtectOpenFile(file *os.File) error      { return file.Chmod(0600) }
func HasPrivateMode(_ string, info os.FileInfo, mode os.FileMode) bool {
	return info.Mode().Perm() == mode
}

// RestrictedAccess preserves consumers that permit read-only owner modes.
func RestrictedAccess(_ string, info os.FileInfo) bool { return info.Mode().Perm()&0077 == 0 }
