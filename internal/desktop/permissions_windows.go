package desktop

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"os"
)

// Windows has no macOS TCC switches. Capture/input use explicit task grants;
// protected/elevated desktops still remain subject to Windows restrictions.
func systemPermissionState(string) SystemPermissionState {
	path, _ := os.Executable()
	return SystemPermissionState{Supported: true, AppPath: path, Permissions: []SystemPermission{}}
}
func requestSystemPermission(context.Context, string, string) (PermissionRequestResult, error) {
	return PermissionRequestResult{}, taskterminal.ErrWindowUnsupported
}
func openSystemPermissionSettings(id string) error {
	if id == "notifications" {
		return windowsOpenURL("ms-settings:notifications")
	}
	return taskterminal.ErrWindowUnsupported
}
func resetSystemPermission(context.Context, string) error { return taskterminal.ErrWindowUnsupported }
func revealPermissionApp() error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	return windowsRevealFile(path)
}
