//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
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
	"os/exec"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func terminalInstalled(id string) bool {
	v := C.CString(id)
	defer C.free(unsafe.Pointer(v))
	return application.InvokeSyncWithResult(func() bool { return C.terminal_installed(v) != 0 })
}
func (s *Service) openPreferredTerminal(ctx context.Context, preference, path string) error {
	if preference == "system" {
		value := C.CString(path)
		defer C.free(unsafe.Pointer(value))
		bundle := application.InvokeSyncWithResult(func() string { v := C.default_terminal(value); defer C.free(unsafe.Pointer(v)); return C.GoString(v) })
		preference = taskterminal.PreferenceForBundle(bundle)
		if preference == "" {
			return taskterminal.ErrUnsupportedDefault
		}
	}
	args, err := taskterminal.OpenArgs(preference, path)
	if err != nil {
		return err
	}
	if preference != "system" && !terminalInstalled(taskterminal.BundleID(preference)) {
		return errors.New(s.text("native.preferredTerminalUnavailable", nil))
	}
	return exec.CommandContext(ctx, "/usr/bin/open", args...).Run()
}
