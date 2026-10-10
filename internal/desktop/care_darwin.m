//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <IOKit/IOKitLib.h>
#import "care_darwin.h"

char *bot_care_sample(void) {
 @autoreleasepool {
  __block uint64_t observedEpoch=0;
  __block NSString *application=@"";
  void (^readWorkspace)(void)=^{
   static uint64_t epoch=0;
   static NSMutableArray *observers;
   if(!observers) {
    observers=[NSMutableArray array];
    for(NSString *name in @[NSWorkspaceWillSleepNotification,NSWorkspaceDidWakeNotification,NSWorkspaceScreensDidSleepNotification,NSWorkspaceScreensDidWakeNotification,NSWorkspaceSessionDidResignActiveNotification,NSWorkspaceSessionDidBecomeActiveNotification]) {
     [observers addObject:[NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:name object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){epoch++;}]];
    }
    for(NSString *name in @[@"com.apple.screenIsLocked",@"com.apple.screenIsUnlocked"]) {
     [observers addObject:[NSDistributedNotificationCenter.defaultCenter addObserverForName:name object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){epoch++;}]];
    }
   }
   observedEpoch=epoch;
   application=NSWorkspace.sharedWorkspace.frontmostApplication.bundleIdentifier ?: @"";
  };
  // Register change observers before sampling. The main-thread part only
  // touches NSWorkspace; CoreGraphics, IOKit and JSON run on the caller.
  if(NSThread.isMainThread)readWorkspace();else dispatch_sync(dispatch_get_main_queue(),readWorkspace);
  // These system reads can wait on CoreGraphics or IOKit. The resident Tick
  // calls from a Go worker; keep only NSWorkspace observation on AppKit.
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
  NSDictionary *sample=@{@"Awake":@(awake),@"Unlocked":unlocked,@"Epoch":@(observedEpoch),@"IdleSeconds":@(idle),@"Application":application};
  NSData *data=[NSJSONSerialization dataWithJSONObject:sample options:0 error:nil];
  return data ? strndup(data.bytes,data.length) : NULL;
 }
}
