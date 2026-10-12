//go:build !darwin

package plugins

import "os"

// Native Desktop World ownership is validated on the shipped macOS host.
// Other platforms retain package/build checks but do not claim GUI ownership.
func lockDesktopOwner(string) (*os.File, error) { return nil, nil }
func desktopOwnerActive(string) bool            { return false }
