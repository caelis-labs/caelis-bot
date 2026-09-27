//go:build !darwin || !cgo

package desktop

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

func systemPermissionState(string) SystemPermissionState {
	return SystemPermissionState{Permissions: []SystemPermission{}}
}
func requestSystemPermission(context.Context, string, string) (PermissionRequestResult, error) {
	return PermissionRequestResult{}, taskterminal.ErrWindowUnsupported
}
func openSystemPermissionSettings(string) error           { return taskterminal.ErrWindowUnsupported }
func resetSystemPermission(context.Context, string) error { return taskterminal.ErrWindowUnsupported }
func revealPermissionApp() error                          { return taskterminal.ErrWindowUnsupported }
