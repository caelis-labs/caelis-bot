#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-task-dock.XXXXXX")
trap 'rm -rf "$directory"' EXIT
cat > "$directory/main.m" <<'OBJC'
#import <Cocoa/Cocoa.h>
#include "task_dock_darwin.m"
#include <assert.h>
static NSDictionary *task(NSString *identifier, NSString *status) {
  return @{@"id":identifier,@"prompt":@"A long task prompt with enough text to wrap within the preview without exceeding its rounded panel.",@"status":status};
}
int main(void) {
 @autoreleasepool {
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
  BotTaskDock *dock=[BotTaskDock new];
  [dock setTasks:@[task(@"one",@"working"),task(@"two",@"completed")]];
  [dock placeWithPet:NSMakeRect(400,300,240,240) bounds:NSMakeRect(0,0,1200,900) visible:YES];
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(48,26)));
  assert(dock.buttons[0].loading);
  if(!NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion)assert([dock.buttons[0].progress animationForKey:@"loading"]);
  [dock expand];
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(102,52)));
  assert(dock.buttons[0].loading && !dock.buttons[1].loading);
  for(BotTaskButton *button in dock.buttons) {
    NSRect rect=[button convertRect:button.bounds toView:dock.window.contentView];
    assert(NSContainsRect(dock.window.contentView.bounds,rect));
  }
  [dock setTasks:@[task(@"two",@"working"),task(@"one",@"completed")]];
  assert([dock.tasks[0][@"id"] isEqual:@"one"] && !dock.buttons[0].loading && dock.buttons[1].loading);
  assert(![dock.buttons[0].progress animationForKey:@"loading"]);
  dock.buttons[1].animateLoading=NO;[dock.buttons[1] updateProgress];
  assert(dock.buttons[1].loading && ![dock.buttons[1].progress animationForKey:@"loading"]);
  [dock showPrompt:[dock promptAt:0]];
  assert(NSEqualSizes(dock.preview.contentView.bounds.size,NSMakeSize(320,70)));
  assert(NSContainsRect(dock.preview.contentView.bounds,dock.prompt.frame));
  NSString *output=NSProcessInfo.processInfo.environment[@"BOT_TASK_DOCK_CAPTURE"];
  if(output.length) {
    [dock.window.contentView display];
    NSBitmapImageRep *image=[dock.window.contentView bitmapImageRepForCachingDisplayInRect:dock.window.contentView.bounds];
    [dock.window.contentView cacheDisplayInRect:dock.window.contentView.bounds toBitmapImageRep:image];
    [[image representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:output atomically:YES];
  }
  [dock collapse];
  assert([dock.tasks[0][@"id"] isEqual:@"two"]);
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(48,26)));
  [dock placeWithPet:NSZeroRect bounds:NSZeroRect visible:NO];
  assert(!dock.window.visible && !dock.buttons[0].loading && ![dock.buttons[0].progress animationForKey:@"loading"]);
  [dock stop];
  puts("Task dock: bounds, live status, stable hover order, loading, reduced motion and hidden lifecycle passed.");
 }
 return 0;
}
OBJC
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa -framework QuartzCore "$directory/main.m" -o "$directory/fixture"
"$directory/fixture"
