#!/usr/bin/env bash
# Account-free AppKit regression; only this process's disposable windows change.
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-pet-visibility.XXXXXX")
trap 'rm -rf "$directory"' EXIT
# Compile the production entrypoint and ordering method verbatim. Unrelated
# renderer/pose collaborators below are inert; application/window ordering is
# real AppKit. The optional source is only for the before-fix negative control.
python3 - "${1:-internal/desktop/native_darwin.m}" "$directory" <<'PY'
import pathlib, sys
source = pathlib.Path(sys.argv[1]).read_text()
for name, start, end in [
    ("apply.inc", "void bot_apply(", "\nvoid bot_bubble("),
    ("show.inc", "- (void)showPetWithoutActivation {", "\n- (void)finishDrag {"),
]:
    begin = source.index(start)
    pathlib.Path(sys.argv[2], name).write_text(source[begin:source.index(end, begin)])
PY
cat > "$directory/main.m" <<'OBJC'
#import <Cocoa/Cocoa.h>
#include <assert.h>
@interface ProbeApplication : NSApplication
@property NSUInteger unhideCalls;
@end
@implementation ProbeApplication
- (void)unhideWithoutActivation { self.unhideCalls++; [super unhideWithoutActivation]; }
@end
@protocol ProbeInput
- (void)cancelInteraction;
@end
@interface BotHost : NSObject
@property BOOL visible;
@property NSPanel *pet;
@property NSPanel *bubble;
@property id<ProbeInput> inputView;
@property NSTimer *contextTimer;
@property id lastContext;
@end
@implementation BotHost
- (void)placeX:(double)x y:(double)y scale:(double)scale { [self.pet setFrame:NSMakeRect(x,y,180*scale,240*scale) display:YES]; }
- (void)updateBubble {}
- (void)updateHit {}
- (void)trace:(NSString *)event {}
#include "show.inc"
@end
static void bot_js(NSWindow *window, NSString *js) {}
#include "apply.inc"
static void drain(void) {
    NSDate *until=[NSDate dateWithTimeIntervalSinceNow:.15];
    while(until.timeIntervalSinceNow>0) [NSRunLoop.currentRunLoop runMode:NSDefaultRunLoopMode beforeDate:until];
}
static NSWindow *window(NSString *title) {
    NSWindow *w=[[NSWindow alloc] initWithContentRect:NSMakeRect(50,80,320,100) styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
    w.title=title;w.releasedWhenClosed=NO;return w;
}
static NSInteger windowIndex(NSWindow *window) {
    NSArray *all=CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements,kCGNullWindowID));
    NSInteger i=0;for(NSDictionary *w in all){if([w[(id)kCGWindowNumber] intValue]==window.windowNumber)return i;i++;}return -1;
}
int main(void) {@autoreleasepool{
    ProbeApplication *app=[ProbeApplication sharedApplication];[app setActivationPolicy:NSApplicationActivationPolicyAccessory];
    NSWindow *chat=window(@"Chat ordering fixture"),*settings=window(@"Settings ordering fixture");
    BotHost *host=[BotHost new];host.pet=[[NSPanel alloc] initWithContentRect:NSMakeRect(50,50,180,240) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    host.pet.level=NSFloatingWindowLevel;host.pet.ignoresMouseEvents=YES;host.pet.releasedWhenClosed=NO;
    [chat orderBack:nil];[settings orderBack:nil];bot_apply((__bridge void *)host,50,50,1,1);drain();
    [chat orderBack:nil];[settings orderBack:nil];drain();
    NSInteger chatBefore=windowIndex(chat),settingsBefore=windowIndex(settings);pid_t front=NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier;
    NSUInteger unhide=app.unhideCalls;
    for(int i=0;i<12;i++){bot_apply((__bridge void *)host,50+i,50,1,1);drain();}
    NSInteger chatAfter=windowIndex(chat),settingsAfter=windowIndex(settings);
    printf("background: chat=%ld->%ld settings=%ld->%ld active=%d key=%d/%d unhide=%lu\n",(long)chatBefore,(long)chatAfter,(long)settingsBefore,(long)settingsAfter,app.active,chat.keyWindow,settings.keyWindow,(unsigned long)(app.unhideCalls-unhide));fflush(stdout);
    assert(app.unhideCalls==unhide);assert(chatBefore>=0 && settingsBefore>=0);
    assert(chatAfter==chatBefore && settingsAfter==settingsBefore);
    assert(!app.active && !chat.keyWindow && !settings.keyWindow && NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==front);
    [chat orderOut:nil];[settings orderOut:nil];
    bot_apply((__bridge void *)host,50,50,1,0);assert(!host.pet.visible);
    bot_apply((__bridge void *)host,50,50,1,1);assert(host.pet.visible && !chat.visible && !settings.visible);
    // Explicit contextual presentation still orders the selected window in.
    [chat makeKeyAndOrderFront:nil];assert(chat.visible);
    [settings makeKeyAndOrderFront:nil];assert(settings.visible);
    [chat orderOut:nil];[settings orderOut:nil];[host.pet orderOut:nil];
    [chat close];[settings close];[host.pet close];
    puts("PASS: repeated pet layout preserves contextual order; closed windows stay closed; explicit opens work.");
}return 0;}
OBJC
clang -fobjc-arc -fblocks -I "$directory" -framework Cocoa "$directory/main.m" -o "$directory/fixture"
"$directory/fixture"
