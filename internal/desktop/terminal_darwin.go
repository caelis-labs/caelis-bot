//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework UniformTypeIdentifiers
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
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
   NSString *filePath=[NSString stringWithUTF8String:path];
   NSURL *app=[NSWorkspace.sharedWorkspace URLForApplicationToOpenURL:[NSURL fileURLWithPath:filePath]];
   // Permission reads use a nonexistent .command placeholder. The URL query
   // requires a real file, while a content-type query needs no file or launch.
   if(!app) {
     UTType *type=[UTType typeWithFilenameExtension:filePath.pathExtension];
     if(type)app=[NSWorkspace.sharedWorkspace URLForApplicationToOpenContentType:type];
   }
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
	return application.InvokeSyncWithResult(func() string { return readDefaultTerminalBundle(path) })
}

// The read-only native leaf is also usable without a running Wails window.
func readDefaultTerminalBundle(path string) string {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	v := C.default_terminal(value)
	defer C.free(unsafe.Pointer(v))
	return C.GoString(v)
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
		return nil, taskterminal.NotLaunched(err)
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
