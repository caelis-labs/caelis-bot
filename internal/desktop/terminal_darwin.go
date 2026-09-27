//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <string.h>
#include "permission_capture_darwin.h"
static int task_snapshot_status(void) {
 if (@available(macOS 14.0,*)) return !bot_screen_capture_denied() && CGPreflightScreenCaptureAccess()?2:1;
 return 0;
}
static int terminal_installed(const char *bundle) {
 @autoreleasepool { return [NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:[NSString stringWithUTF8String:bundle]] != nil; }
}
static char *default_terminal(const char *path) {
 @autoreleasepool {
   NSURL *app=[NSWorkspace.sharedWorkspace URLForApplicationToOpenURL:[NSURL fileURLWithPath:[NSString stringWithUTF8String:path]]];
   NSString *identifier=app ? [NSBundle bundleWithURL:app].bundleIdentifier : nil;
   return strdup((identifier ?: @"").UTF8String);
 }
}
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func taskSnapshotStatus() string {
	switch C.task_snapshot_status() {
	case 2:
		return "available"
	case 1:
		return "permissionRequired"
	default:
		return "unsupported"
	}
}
func configureTaskSnapshots() error {
	if taskSnapshotStatus() == "unsupported" {
		return taskterminal.ErrWindowUnsupported
	}
	_, err := requestSystemPermission(context.Background(), "screenCapture", "")
	return err
}

func defaultTerminalBundle(path string) string {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	return application.InvokeSyncWithResult(func() string { v := C.default_terminal(value); defer C.free(unsafe.Pointer(v)); return C.GoString(v) })
}

func terminalInstalled(id string) bool {
	v := C.CString(id)
	defer C.free(unsafe.Pointer(v))
	return application.InvokeSyncWithResult(func() bool { return C.terminal_installed(v) != 0 })
}
func (s *Service) openManagedTerminal(ctx context.Context, preference, path string) (taskterminal.Window, error) {
	if preference == "system" {
		bundle := defaultTerminalBundle(path)
		if known := taskterminal.PreferenceForBundle(bundle); known != "" {
			return taskterminal.OpenWindow(ctx, known, path)
		}
		return taskterminal.OpenApplication(ctx, bundle, "system", path)
	}
	preference, err := s.resolveTerminalPreference(preference, path)
	if err != nil {
		return nil, err
	}
	return taskterminal.OpenWindow(ctx, preference, path)
}

func (s *Service) resolveTerminalPreference(preference, path string) (string, error) {
	if preference == "system" {
		bundle := defaultTerminalBundle(path)
		preference = taskterminal.PreferenceForBundle(bundle)
		if preference == "" {
			return "", taskterminal.ErrUnsupportedDefault
		}
	}
	if preference != "system" && !terminalInstalled(taskterminal.BundleID(preference)) {
		return "", errors.New(s.text("native.preferredTerminalUnavailable", nil))
	}
	return preference, nil
}
