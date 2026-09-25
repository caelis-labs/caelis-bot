//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <IOKit/IOKitLib.h>
#import "care_darwin.h"

char *bot_care_sample(void) {
 __block char *result=NULL;
 void (^read)(void)=^{ @autoreleasepool {
  NSDictionary *session=CFBridgingRelease(CGSessionCopyCurrentDictionary());
  BOOL console=[session[(__bridge NSString *)kCGSessionOnConsoleKey] boolValue];
  BOOL login=[session[(__bridge NSString *)kCGSessionLoginDoneKey] boolValue];
  id unlocked=NSNull.null;
  // XNU's registry publishes an explicit Boolean. Missing/changed metadata
  // stays unknown; session-active alone never proves that the screen is unlocked.
  io_registry_entry_t root=IORegistryGetRootEntry(kIOMainPortDefault);
  if(root) {
   CFTypeRef locked=IORegistryEntryCreateCFProperty(root,CFSTR("IOConsoleLocked"),kCFAllocatorDefault,0);
   if(locked && CFGetTypeID(locked)==CFBooleanGetTypeID())unlocked=@(!CFBooleanGetValue((CFBooleanRef)locked) && console && login);
   if(locked)CFRelease(locked);IOObjectRelease(root);
  }
  BOOL awake=console && login && !CGDisplayIsAsleep(CGMainDisplayID());
  double idle=CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateCombinedSessionState,kCGAnyInputEventType);
  if(!isfinite(idle)||idle<0){awake=NO;idle=0;}
  NSDictionary *sample=@{@"Awake":@(awake),@"Unlocked":unlocked,@"IdleSeconds":@(idle),@"Application":NSWorkspace.sharedWorkspace.frontmostApplication.bundleIdentifier ?: @""};
  NSData *data=[NSJSONSerialization dataWithJSONObject:sample options:0 error:nil];
  if(data)result=strndup(data.bytes,data.length);
 }};
 if(NSThread.isMainThread)read();else dispatch_sync(dispatch_get_main_queue(),read);
 return result;
}
