//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices -framework Security
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import <Security/Security.h>
#include <stdlib.h>
#include "permission_capture_darwin.h"

static char *permission_app_path(void) { return strdup(NSBundle.mainBundle.bundlePath.UTF8String); }
static char *permission_app_id(void) { return strdup((NSBundle.mainBundle.bundleIdentifier?:@"").UTF8String); }
static int permission_development(void) {
 SecCodeRef code=NULL;CFDictionaryRef info=NULL;int result=1;
 if(SecCodeCopySelf(kSecCSDefaultFlags,&code)==errSecSuccess &&
    SecCodeCopySigningInformation(code,kSecCSSigningInformation,&info)==errSecSuccess) {
   NSNumber *flags=((__bridge NSDictionary *)info)[(__bridge NSString *)kSecCodeInfoFlags];
   result=!flags || (flags.unsignedIntValue & kSecCodeSignatureAdhoc)!=0;
 }
 if(info)CFRelease(info);if(code)CFRelease(code);return result;
}
static int permission_accessibility(void) { return AXIsProcessTrusted(); }
static int computer_use_supported(void) { if (@available(macOS 13.5, *)) return 1; return 0; }
static int permission_settings(const char *category) {
 NSString *url=[@"x-apple.systempreferences:com.apple.preference.security?Privacy_" stringByAppendingString:[NSString stringWithUTF8String:category]];
 return [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:url]];
}
static void permission_accessibility_request(void) {
 // This prompt is asynchronous. Opening Settings immediately can obscure it
 // before macOS has registered this app. Let the system prompt own navigation.
 AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)@{(__bridge NSString *)kAXTrustedCheckOptionPrompt:@YES});
}
static int permission_notification_settings(void) {
 return [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:@"x-apple.systempreferences:com.apple.Notifications-Settings.extension"]];
}
static void permission_reveal(void) {
 [NSWorkspace.sharedWorkspace activateFileViewerSelectingURLs:@[NSBundle.mainBundle.bundleURL]];
}
static long permission_automation(const char *bundle, int request) {
 @autoreleasepool {
  NSString *identifier=[NSString stringWithUTF8String:bundle];
  // Reads must not launch the target or prompt. Not running is unknown, not denied.
  if([NSRunningApplication runningApplicationsWithBundleIdentifier:identifier].count==0) return procNotFound;
  NSAppleEventDescriptor *target=[NSAppleEventDescriptor descriptorWithBundleIdentifier:identifier];
  return AEDeterminePermissionToAutomateTarget(target.aeDesc,typeWildCard,typeWildCard,request!=0);
 }
}
// Called off the main thread only, following an explicit Settings click. Starting
// the selected application requests no shell execution and does not create a task.
static int permission_start_terminal(const char *bundle) {
 @autoreleasepool {
  NSString *identifier=[NSString stringWithUTF8String:bundle];
  if([NSRunningApplication runningApplicationsWithBundleIdentifier:identifier].count)return 1;
  dispatch_semaphore_t finished=dispatch_semaphore_create(0);
  __block BOOL started=NO;
  dispatch_async(dispatch_get_main_queue(),^{
   NSURL *url=[NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:identifier];
   if(!url){dispatch_semaphore_signal(finished);return;}
   NSWorkspaceOpenConfiguration *config=[NSWorkspaceOpenConfiguration configuration];
   config.activates=YES;
   [NSWorkspace.sharedWorkspace openApplicationAtURL:url configuration:config completionHandler:^(NSRunningApplication *app,NSError *error){
    started=app!=nil && error==nil;dispatch_semaphore_signal(finished);
   }];
  });
  if(dispatch_semaphore_wait(finished,dispatch_time(DISPATCH_TIME_NOW,15*NSEC_PER_SEC)))return 0;
  return started;
 }
}
*/
import "C"

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"time"
	"unsafe"

	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func computerUseSupported() bool { return C.computer_use_supported() != 0 }

func permissionTerminalBundle(preference string) string {
	if preference == "system" {
		// Same extension-based default as opening a task. No file is written.
		preference = taskterminal.PreferenceForBundle(defaultTerminalBundle("/tmp/Caelis Bot.command"))
	}
	return taskterminal.BundleID(preference)
}
func systemPermissionState(preference string) SystemPermissionState {
	state := SystemPermissionState{Supported: true, Permissions: []SystemPermission{}}
	application.InvokeSync(func() {
		v := C.permission_app_path()
		defer C.free(unsafe.Pointer(v))
		state.AppPath = C.GoString(v)
		state.Development = C.permission_development() != 0
	})
	ax := "permissionRequired"
	if C.permission_accessibility() != 0 {
		ax = "authorized"
	}
	capture := taskSnapshotStatus()
	if capture == "available" {
		capture = "authorized"
	}
	state.Permissions = append(state.Permissions, SystemPermission{ID: "accessibility", Status: ax}, SystemPermission{ID: "screenCapture", Status: capture})
	bundle := permissionTerminalBundle(preference)
	automation := SystemPermission{ID: "automation", Status: "unsupported"}
	if bundle != "" {
		v := C.CString(bundle)
		defer C.free(unsafe.Pointer(v))
		automation.Status = automationPermissionStatus(int(C.permission_automation(v, 0)))
		automation.Target = map[string]string{"com.apple.Terminal": "Terminal", "com.googlecode.iterm2": "iTerm2", "com.mitchellh.ghostty": "Ghostty"}[bundle]
	}
	state.Permissions = append(state.Permissions, automation)
	return state
}
func automationPermissionStatus(code int) string {
	switch code {
	case 0:
		return "authorized"
	case -1744:
		return "notDetermined"
	case -1743, -10004:
		return "denied"
	case -600:
		return "notRunning"
	}
	return "unavailable"
}
func requestSystemPermission(ctx context.Context, id, preference string) (PermissionRequestResult, error) {
	if err := ctx.Err(); err != nil {
		return PermissionRequestResult{}, err
	}
	switch id {
	case "screenCapture":
		if taskSnapshotStatus() == "unsupported" {
			return PermissionRequestResult{}, taskterminal.ErrWindowUnsupported
		}
		// This is intentionally not CGRequestScreenCaptureAccess. ScreenCaptureKit
		// registers consent without capturing pixels, starting a stream or a worker.
		log.Print("System permission request: screenCapture started")
		handle := C.bot_screen_permission_request()
		defer C.bot_screen_permission_release(handle)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		result, err := waitScreenPermission(ctx, func() (int, int64) {
			var code C.long
			status := C.bot_screen_permission_poll(handle, &code)
			return int(status), int64(code)
		})
		log.Printf("System permission request: screenCapture state=%s error=%v", result.State, err)
		return result, err
	case "accessibility":
		if C.permission_accessibility() != 0 {
			return PermissionRequestResult{State: "authorized"}, nil
		}
		application.InvokeSync(func() { C.permission_accessibility_request() })
		return PermissionRequestResult{State: "requested"}, nil
	case "automation":
		bundle := permissionTerminalBundle(preference)
		if bundle == "" {
			return PermissionRequestResult{}, taskterminal.ErrWindowUnsupported
		}
		v := C.CString(bundle)
		defer C.free(unsafe.Pointer(v))
		if automationPermissionStatus(int(C.permission_automation(v, 0))) == "authorized" {
			return PermissionRequestResult{State: "authorized"}, nil
		}
		if C.permission_start_terminal(v) == 0 {
			return PermissionRequestResult{}, errors.New("terminal could not start")
		}
		if err := ctx.Err(); err != nil {
			return PermissionRequestResult{}, err
		}
		switch automationPermissionStatus(int(C.permission_automation(v, 1))) {
		case "authorized":
			return PermissionRequestResult{State: "authorized"}, nil
		case "denied":
			return PermissionRequestResult{State: "settingsRequired"}, nil
		}
		return PermissionRequestResult{}, errors.New("automation permission is not confirmed")
	}
	return PermissionRequestResult{}, errors.New("unsupported permission")
}
func openSystemPermissionSettings(id string) error {
	category := map[string]string{"accessibility": "Accessibility", "screenCapture": "ScreenCapture", "automation": "Automation"}[id]
	if category == "" && id != "notifications" {
		return errors.New("unsupported permission")
	}
	opened := application.InvokeSyncWithResult(func() bool {
		if id == "notifications" {
			return C.permission_notification_settings() != 0
		}
		value := C.CString(category)
		defer C.free(unsafe.Pointer(value))
		return C.permission_settings(value) != 0
	})
	if !opened {
		return errors.New("system permission settings could not open")
	}
	return nil
}

func resetSystemPermission(ctx context.Context, service string) error {
	id := application.InvokeSyncWithResult(func() string { v := C.permission_app_id(); defer C.free(unsafe.Pointer(v)); return C.GoString(v) })
	_, expectedID := applicationIdentity()
	if id != expectedID {
		return errors.New("permission reset requires the Caelis Bot application bundle")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// No shell, sudo, broad All, other bundle IDs or direct TCC database access.
	if err := exec.CommandContext(ctx, "/usr/bin/tccutil", "reset", service, id).Run(); err != nil {
		return errors.New("system permission reset failed")
	}
	return nil
}
func revealPermissionApp() error {
	application.InvokeSync(func() { C.permission_reveal() })
	return nil
}
