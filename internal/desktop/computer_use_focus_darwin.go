//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include <dlfcn.h>

typedef AXError (*BotAXWindowID)(AXUIElementRef, CGWindowID *);
static BOOL bot_cua_window_id(AXUIElementRef window, CGWindowID *wid) {
 BotAXWindowID getID=(BotAXWindowID)dlsym(RTLD_DEFAULT,"_AXUIElementGetWindow");
 return getID && getID(window,wid)==kAXErrorSuccess;
}
static AXUIElementRef bot_cua_focus_target(int pid, unsigned int wid) {
 @autoreleasepool {
  if(!AXIsProcessTrusted())return NULL;
  NSArray *windows=CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionIncludingWindow,wid));
  BOOL owned=NO;
  for(NSDictionary *row in windows) {
   if([row[(id)kCGWindowNumber] unsignedIntValue]==wid && [row[(id)kCGWindowOwnerPID] intValue]==pid && [row[(id)kCGWindowLayer] intValue]==0)owned=YES;
  }
  if(!owned)return NULL;
  AXUIElementRef app=AXUIElementCreateApplication(pid);
  AXUIElementSetMessagingTimeout(app,0.25);
  CFTypeRef value=NULL;
  AXUIElementRef target=NULL;
  if(AXUIElementCopyAttributeValue(app,kAXWindowsAttribute,&value)==kAXErrorSuccess && value && CFGetTypeID(value)==CFArrayGetTypeID()) {
   CFArrayRef axWindows=(CFArrayRef)value;
   for(CFIndex i=0;i<CFArrayGetCount(axWindows);i++) {
    AXUIElementRef window=(AXUIElementRef)CFArrayGetValueAtIndex(axWindows,i);
    CGWindowID candidate=0;
    if(bot_cua_window_id(window,&candidate) && candidate==wid) {
     CFTypeRef minimized=NULL;
     AXError e=AXUIElementCopyAttributeValue(window,kAXMinimizedAttribute,&minimized);
     if(e==kAXErrorSuccess && minimized==kCFBooleanFalse)target=(AXUIElementRef)CFRetain(window);
     if(minimized)CFRelease(minimized);
     break;
    }
   }
  }
  if(value)CFRelease(value);
  CFRelease(app);
  return target;
 }
}
static int bot_cua_activate(int pid, AXUIElementRef target) {
 @autoreleasepool {
  NSRunningApplication *app=[NSRunningApplication runningApplicationWithProcessIdentifier:pid];
  if(!app || app.terminated)return 0;
  AXUIElementSetMessagingTimeout(target,0.25);
  if(AXUIElementPerformAction(target,kAXRaiseAction)!=kAXErrorSuccess)return 0;
  return [app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
 }
}
static int bot_cua_focused(int pid, unsigned int wid) {
 @autoreleasepool {
  // Read the WindowServer process state directly. NSWorkspace notifications
  // may lag on a non-main calling thread (including the acceptance CLI).
  ProcessSerialNumber front; pid_t frontPID=0;
  if(GetFrontProcess(&front)!=noErr || GetProcessPID(&front,&frontPID)!=noErr || frontPID!=pid)return 0;
  AXUIElementRef app=AXUIElementCreateApplication(pid);
  AXUIElementSetMessagingTimeout(app,0.25);
  CFTypeRef focused=NULL;
  CGWindowID actual=0;
  BOOL ok=AXUIElementCopyAttributeValue(app,kAXFocusedWindowAttribute,&focused)==kAXErrorSuccess && focused && bot_cua_window_id((AXUIElementRef)focused,&actual) && actual==wid;
  if(focused)CFRelease(focused);
  CFRelease(app);
  // Some AX-poor apps expose AXWindows but omit AXFocusedWindow. WindowServer
  // still authoritatively orders their visible layer-0 windows front to back.
  // A mapped sibling AX focus must never be overridden by this fallback.
  if(!ok && actual==0) {
   NSArray *rows=CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements,kCGNullWindowID));
   for(NSDictionary *row in rows) {
    if([row[(id)kCGWindowOwnerPID] intValue]!=pid || [row[(id)kCGWindowLayer] intValue]!=0)continue;
    CGRect frame; if(!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)row[(id)kCGWindowBounds],&frame) || frame.size.width<1 || frame.size.height<1)continue;
    ok=[row[(id)kCGWindowNumber] unsignedIntValue]==wid;
    break;
   }
  }
  return ok;
 }
}
*/
import "C"

import (
	"context"
	"errors"
	"time"
)

// FocusComputerUseWindow fronts only the live exact window admitted by the
// private Cua adapter. No title matching, launch, typing or fallback is allowed.
// The Bot must observe again before issuing another separately authorized input.
func FocusComputerUseWindow(ctx context.Context, pid int, windowID uint64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if pid <= 0 || pid > 1<<31-1 || windowID == 0 || windowID > 1<<32-1 {
		return errors.New("invalid focus target")
	}
	target := C.bot_cua_focus_target(C.int(pid), C.uint(windowID))
	if target == 0 {
		return errors.New("focus target unavailable")
	}
	defer C.CFRelease(C.CFTypeRef(target))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if C.bot_cua_activate(C.int(pid), target) == 0 {
		return errors.New("window activation failed")
	}
	wait, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for {
		if wait.Err() != nil {
			return wait.Err()
		}
		if C.bot_cua_focused(C.int(pid), C.uint(windowID)) != 0 {
			return nil
		}
		select {
		case <-wait.Done():
			return wait.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
