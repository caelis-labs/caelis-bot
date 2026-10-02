#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/env.sh"
[[ "$(uname -s)" == Darwin ]] || exit 0
directory=$(mktemp -d "${TMPDIR:-/tmp}/bot-task-dock.XXXXXX")
trap 'rm -rf "$directory"' EXIT
cat > "$directory/main.m" <<'OBJC'
#import <Cocoa/Cocoa.h>
#include "task_snapshot_darwin.m"
#include "task_dock_darwin.m"
#include <assert.h>
@interface FixtureCapture : BotTaskSnapshot
@property NSUInteger calls;
@end
@implementation FixtureCapture
- (void)capture:(NSDictionary *)source completion:(void (^)(NSImage *))completion { self.calls++;completion(nil); }
@end
// Synthetic metadata only; never enumerate or capture external windows here.
@interface FixtureSnapshotWindow : NSObject
@property int processID;
@property int windowLayer;
@property uint32_t windowID;
@property CGRect frame;
@property BOOL onScreen;
- (id)owningApplication;
@end
@implementation FixtureSnapshotWindow
- (id)owningApplication { return self; }
@end
static void checkSnapshotSelection(void) {
 if(@available(macOS 14.0,*)) {
  FixtureSnapshotWindow *owned=[FixtureSnapshotWindow new];owned.processID=7;owned.windowID=11;owned.frame=CGRectMake(0,0,600,400);owned.onScreen=NO;
  FixtureSnapshotWindow *other=[FixtureSnapshotWindow new];other.processID=8;other.windowID=12;other.frame=owned.frame;
  NSDictionary *source=@{@"pid":@7};NSUInteger eligible=0;
  assert(taskSnapshotWindow((id)@[owned,other],source,&eligible)==(id)owned);
  other.processID=7;
  assert(!taskSnapshotWindow((id)@[owned,other],source,&eligible) && eligible==2);
  // Distinct geometry is not authority to choose one window in an instance.
  other.frame=CGRectMake(10,20,600,400);
  assert(!taskSnapshotWindow((id)@[owned,other],source,&eligible) && eligible==2);
  other.windowLayer=3;
  assert(taskSnapshotWindow((id)@[owned,other],source,&eligible)==(id)owned);
 }
}
static NSDictionary *task(NSString *identifier, NSString *status) {
  return @{@"id":identifier,@"prompt":@"A long task prompt with enough text to wrap within the preview without exceeding its rounded panel.",@"status":status};
}
static void hoverAt(BotTaskDock *dock, BotTaskStack *stack, NSPoint point, NSUInteger expected) {
  NSPoint location=[stack convertPoint:point toView:nil];
  NSEvent *event=[NSEvent mouseEventWithType:NSEventTypeMouseMoved location:location modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:0 clickCount:0 pressure:0];
  [stack mouseMoved:event];assert(stack.selected==dock.buttons[expected]);
  BotTaskButton *card=dock.buttons[expected];
  NSPoint center=NSMakePoint(NSMidX(card.frame),NSMidY(card.frame));
  NSView *hit=[stack hitTest:[stack convertPoint:center toView:stack.superview]];
  assert(hit==card); // The full hovered card, not a sliver or another task.
  for(NSView *control in card.subviews)if([control isKindOfClass:NSButton.class]) {
    assert(!control.hidden);
    NSPoint controlPoint=[control convertPoint:NSMakePoint(NSMidX(control.bounds),NSMidY(control.bounds)) toView:nil];
    NSEvent *controlEvent=[NSEvent mouseEventWithType:NSEventTypeMouseMoved location:controlPoint modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:1 clickCount:0 pressure:0];
    [stack mouseMoved:controlEvent];assert(stack.selected==card);
    NSPoint inParent=[control convertPoint:NSMakePoint(NSMidX(control.bounds),NSMidY(control.bounds)) toView:stack.superview];
    assert([stack hitTest:inParent]==control);
  }
}
static void checkDenseStack(BotTaskDock *dock) {
  BotTaskStack *stack=(BotTaskStack *)dock.window.contentView.subviews[0];
  assert([stack isKindOfClass:BotTaskStack.class] && !stack.enclosingScrollView);
  NSArray *cards=dock.buttons;NSMutableArray *frames=[NSMutableArray new];
  for(BotTaskButton *card in cards) {
    if(!NSContainsRect(stack.bounds,card.frame))fprintf(stderr,"card %ld vertical %d up %d bounds %s frame %.17g %.17g %.17g %.17g\n",(long)card.tag,stack.vertical,stack.growsUp,NSStringFromRect(stack.bounds).UTF8String,card.frame.origin.x,card.frame.origin.y,card.frame.size.width,card.frame.size.height);
    assert(NSContainsRect(stack.bounds,card.frame));[frames addObject:[NSValue valueWithRect:card.frame]];
  }
  [cards.firstObject mouseEntered:[NSEvent new]];
  hoverAt(dock,stack,NSMakePoint(NSMidX([cards[0] frame]),NSMidY([cards[0] frame])),0);
  for(NSUInteger step=1;step<cards.count*2-1;step++) {
    BOOL forward=step<cards.count;
    NSUInteger i=forward?step:cards.count*2-2-step;
    NSRect current=[cards[i] frame],previous=[cards[forward?i-1:i+1] frame];
    NSPoint point=NSMakePoint(NSMidX(current),NSMidY(current));
    BOOL upper=forward!= (stack.vertical && !stack.growsUp);
    if(stack.vertical)point.y=upper?(NSMaxY(previous)+NSMaxY(current))/2:(NSMinY(previous)+NSMinY(current))/2;
    else point.x=upper?(NSMaxX(previous)+NSMaxX(current))/2:(NSMinX(previous)+NSMinX(current))/2;
    hoverAt(dock,stack,point,i);
    assert(NSEqualRects([frames[i] rectValue],[cards[i] frame]));
  }
}
static void checkAdaptiveGeometry(void) {
  NSRect screen=NSMakeRect(0,0,1200,900),safe=NSInsetRect(screen,8,8);
  BotTaskDockPlacement center=taskDockPlacement(NSMakeRect(480,450,240,240),screen,3,YES);
  assert(!center.vertical && !center.growsUp);
  NSPoint corners[]={NSMakePoint(0,0),NSMakePoint(960,0),NSMakePoint(0,660),NSMakePoint(960,660)};
  for(NSUInteger i=0;i<4;i++) {
    NSRect pet=NSMakeRect(corners[i].x,corners[i].y,240,240);
    BotTaskDockPlacement p=taskDockPlacement(pet,screen,12,YES);
    assert(p.vertical && p.growsUp==(i<2));assert(NSContainsRect(safe,p.frame));assert(!NSIntersectsRect(pet,p.frame));
  }
  BotTaskDockPlacement one=taskDockPlacement(NSMakeRect(480,0,240,240),screen,1,YES);
  assert(!one.vertical && one.growsUp && NSMinY(one.frame)>=240);
  NSRect screens[]={NSMakeRect(-1600,900,1200,900),NSMakeRect(0,0,320,680),NSMakeRect(0,-1300,600,1200)};
  for(NSUInteger s=0;s<3;s++)for(int x=0;x<=4;x++)for(int y=0;y<=4;y++)for(int n=1;n<=100;n++) {
    NSRect b=screens[s];NSRect pet=NSMakeRect(NSMinX(b)+x*(b.size.width-240)/4,NSMinY(b)+y*(b.size.height-240)/4,240,240);
    BotTaskDockPlacement p=taskDockPlacement(pet,b,n,YES);
    assert(NSContainsRect(NSInsetRect(b,8,8),p.frame));
    p=taskDockPlacement(pet,b,n,NO);assert(NSContainsRect(NSInsetRect(b,8,8),p.frame));
  }
}
int main(void) {
  checkSnapshotSelection();
  if(@available(macOS 14.0,*)) {
  assert(!bot_screen_capture_denied());
  assert(!taskSnapshotDenied([NSError errorWithDomain:NSCocoaErrorDomain code:-3801 userInfo:nil]));
  assert(!bot_screen_capture_denied());
  assert(taskSnapshotDenied([NSError errorWithDomain:SCStreamErrorDomain code:SCStreamErrorUserDeclined userInfo:nil]));
  __block BOOL skipped=NO;
  [[BotTaskSnapshot new] capture:@{} completion:^(NSImage *image){assert(!image);skipped=YES;}];
  assert(skipped && bot_screen_capture_denied()); // No enumeration, prompt or capture after a refusal.
  bot_screen_capture_set_denied(0);
  }
 @autoreleasepool {
  checkAdaptiveGeometry();
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
  BotTaskDock *dock=[BotTaskDock new];
  __block NSString *notice=@"";__block BOOL noticePending=NO;
  dock.notice=^(NSString *message,BOOL pending){notice=message;noticePending=pending;};
  NSData *language=[NSData dataWithContentsOfFile:@"internal/i18n/locales/zh-CN/native.json"];
  dock.language=[NSJSONSerialization JSONObjectWithData:language options:0 error:nil];
  NSMutableDictionary *remoteTask=[task(@"one",@"working") mutableCopy];remoteTask[@"targetLabel"]=@"Fixture Worker location";
  [dock setTasks:@[remoteTask,task(@"two",@"completed")]];
  [dock placeWithPet:NSMakeRect(400,300,240,240) bounds:NSMakeRect(0,0,1200,900) visible:YES];
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(62,28)));
  assert(dock.buttons[0].loading);
  assert(!dock.window.hasShadow && ![dock.window.contentView isKindOfClass:NSVisualEffectView.class]);
  assert(dock.buttons[0].title.length==0 && dock.buttons[0].dots.count==3);
  for(CALayer *dot in dock.buttons[0].dots)assert(CGRectContainsRect(dock.buttons[0].bounds,dot.frame));
  if(!NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion)assert([dock.buttons[0].dots[0] animationForKey:@"pulse"]);
  for(CALayer *dot in dock.buttons[0].dots) {
    CAAnimationGroup *pulse=(CAAnimationGroup *)[dot animationForKey:@"pulse"];
    if(pulse)for(CAAnimation *child in pulse.animations)assert(child.duration>0);
  }
  NSString *collapsed=NSProcessInfo.processInfo.environment[@"BOT_TASK_DOTS_CAPTURE"];
  if(collapsed.length) {
    [dock.window.contentView display];
    NSBitmapImageRep *still=[dock.window.contentView bitmapImageRepForCachingDisplayInRect:dock.window.contentView.bounds];
    [dock.window.contentView cacheDisplayInRect:dock.window.contentView.bounds toBitmapImageRep:still];
    [[still representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:collapsed atomically:YES];
  }
  // Hover can win the race with the collapsed entry's tracked mouse-up.
  // A late entry action must keep the cards open and must never open a terminal.
  BotTaskButton *entry=dock.buttons[0];__block NSUInteger entryOpens=0;
  dock.openTask=^(NSString *identifier){entryOpens++;};
  [entry mouseEntered:[NSEvent new]];[dock.openTimer fire];
  assert(dock.expanded);NSArray *expandedButtons=dock.buttons;
  [dock updateActivity];
  assert([dock.buttons[0].statusText containsString:@"Fixture Worker location"]);
  assert([dock.buttons[0].accessibilityValue containsString:@"Fixture Worker location"]);

  // Native terminal state does not imply that the original cancel receipt is known.
  NSArray *terminalUnknown=@[@{@"id":@"one",@"prompt":remoteTask[@"prompt"],@"status":@"interrupted",@"outcome":@"unknown",@"targetLabel":@"Fixture Worker location"},task(@"two",@"completed")];
  [dock setTasks:terminalUnknown];[dock updateActivity];
  assert([dock.buttons[0].statusText containsString:@"回执未确认"]);
  assert([dock.buttons[0].accessibilityValue containsString:@"已停止"]);
  assert(!dock.buttons[0].loading);
  assert([dock.tasks[0][@"status"] isEqual:@"interrupted"]);
  [dock setTasks:@[remoteTask,task(@"two",@"completed")]];[dock updateActivity];

  [entry performClick:nil];
  assert(dock.expanded && dock.buttons==expandedButtons && entryOpens==0);
  // A release inherited from the old entry is not a press on a new card.
  [dock.buttons[0] mouseUp:[NSEvent new]];assert(entryOpens==0);
  dock.openTask=nil;
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(412,196)));
  assert(dock.buttons[0].loading && !dock.buttons[1].loading);
  assert(!dock.window.hasShadow && ![dock.window.contentView isKindOfClass:NSVisualEffectView.class]);
  assert(NSIntersectsRect(dock.buttons[0].frame,dock.buttons[1].frame));
  for(BotTaskButton *button in dock.buttons) {
    NSRect rect=[button convertRect:button.bounds toView:dock.window.contentView];
    assert(NSContainsRect(dock.window.contentView.bounds,rect));
  }
  BotTaskStack *stack=(BotTaskStack *)dock.window.contentView.subviews[0];
  NSRect firstFrame=dock.buttons[0].frame;
  NSPoint p1=[stack convertPoint:NSMakePoint(NSMinX(firstFrame)+25,100) toView:nil];
  NSEvent *motion=[NSEvent mouseEventWithType:NSEventTypeMouseMoved location:p1 modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:0 clickCount:0 pressure:0];
  [stack mouseMoved:motion];assert(stack.selected==dock.buttons[0] && dock.buttons[0].hovered);
  NSPoint p2=[stack convertPoint:NSMakePoint(NSMaxX(dock.buttons[1].frame)-20,100) toView:nil];
  motion=[NSEvent mouseEventWithType:NSEventTypeMouseMoved location:p2 modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:1 clickCount:0 pressure:0];
  [stack mouseMoved:motion];assert(stack.selected==dock.buttons[1] && dock.buttons[1].hovered && !dock.buttons[0].hovered);
  assert(NSEqualRects(firstFrame,dock.buttons[0].frame));
  [dock setTasks:@[task(@"two",@"working"),task(@"one",@"completed")]];
  assert([dock.tasks[0][@"id"] isEqual:@"one"] && !dock.buttons[0].loading && dock.buttons[1].loading);
  assert(![dock.buttons[0].dots[0] animationForKey:@"pulse"]);
  NSRect hit=dock.buttons[0].frame; [dock.buttons[0] mouseEntered:[NSEvent new]];
  assert(dock.buttons[0].hovered && NSEqualRects(hit,dock.buttons[0].frame));
  [dock.buttons[0] mouseExited:[NSEvent new]]; assert(!dock.buttons[0].hovered);
  dock.buttons[1].animateLoading=NO;[dock.buttons[1] updateProgress];
  assert(dock.buttons[1].loading && ![dock.buttons[1].dots[0] animationForKey:@"pulse"]);
  NSUInteger windowsBeforeNotice=NSApp.windows.count;
  [dock showFailure:[dock promptAt:0]];
  assert([notice isEqual:[dock promptAt:0]] && !noticePending);
  assert(NSApp.windows.count==windowsBeforeNotice); // Feedback creates no second panel.
  NSString *output=NSProcessInfo.processInfo.environment[@"BOT_TASK_DOCK_CAPTURE"];
  if(output.length) {
    [dock.window.contentView display];
    NSBitmapImageRep *image=[dock.window.contentView bitmapImageRepForCachingDisplayInRect:dock.window.contentView.bounds];
    [dock.window.contentView cacheDisplayInRect:dock.window.contentView.bounds toBitmapImageRep:image];
    [[image representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:output atomically:YES];
  }
  __block NSString *lockedID=nil;__block BOOL lockValue=NO;
  dock.lockTask=^(NSString *identifier,BOOL locked){lockedID=identifier;lockValue=locked;};
  [dock.buttons[0] mouseEntered:[NSEvent new]];
  for(NSView *view in dock.buttons[0].subviews)if([view isKindOfClass:NSButton.class] && [(NSButton *)view action]==@selector(changeLock:))[(NSButton *)view performClick:nil];
  assert([lockedID isEqual:@"one"] && lockValue);
  [dock collapse];
  assert([dock.tasks[0][@"id"] isEqual:@"two"]);
  assert(NSEqualSizes(dock.window.contentView.bounds.size,NSMakeSize(62,28)));
  [dock expand];
  __block NSString *removed=nil; __block BOOL opened=NO;
  dock.unpinTask=^(NSString *identifier){removed=identifier;};
  dock.openTask=^(NSString *identifier){opened=YES;};
  assert(dock.buttons[0].menu==nil);
  for(NSView *view in dock.buttons[0].subviews)if([view isKindOfClass:NSButton.class] && [(NSButton *)view action]==@selector(removePin:))[dock removePin:view];
  assert([removed isEqual:@"two"] && !opened && !dock.expanded);
  // Upward dismissal is one gesture; locked tasks remain resident.
  [dock setTasks:@[task(@"one",@"working"),task(@"two",@"completed")]];[dock expand];removed=nil;
  [dock swipeTask:@"one" delta:40 phase:NSEventPhaseBegan];
  [dock swipeTask:@"one" delta:30 phase:NSEventPhaseEnded];assert([removed isEqual:@"one"] && !opened);
  NSMutableDictionary *locked=[task(@"one",@"working") mutableCopy];locked[@"locked"]=@YES;
  [dock setTasks:@[locked,task(@"two",@"completed")]];[dock expand];removed=nil;
  [dock swipeTask:@"one" delta:100 phase:NSEventPhaseBegan];[dock swipeTask:@"one" delta:0 phase:NSEventPhaseEnded];assert(removed==nil);
  [dock collapse];[dock setTasks:@[task(@"one",@"working"),task(@"two",@"completed")]];[dock expand];
  __block NSString *moved=nil,*beforeID=nil,*placed=nil;__block NSPoint drop;
  dock.reorderTask=^(NSString *identifier,NSString *before){moved=identifier;beforeID=before;};
  dock.placeTask=^(NSString *identifier,NSPoint point){placed=identifier;drop=point;};
  NSPoint start=NSMakePoint(NSMinX(dock.window.frame)+70,NSMinY(dock.window.frame)+100);
  [dock dragTask:@"one" phase:0 point:start];
  [dock dragTask:@"one" phase:2 point:NSMakePoint(NSMaxX(dock.window.frame)-5,start.y)];
  assert([moved isEqual:@"one"] && [beforeID isEqual:@""] && [dock.tasks.lastObject[@"id"] isEqual:@"one"] && !opened);
  start=NSMakePoint(NSMidX(dock.window.frame),NSMinY(dock.window.frame)+100);
  [dock dragTask:@"one" phase:0 point:start];NSPoint desktop=NSMakePoint(NSMaxX(dock.window.frame)+120,start.y+20);
  [dock dragTask:@"one" phase:2 point:desktop];assert([placed isEqual:@"one"] && NSEqualPoints(drop,desktop) && !dock.dragging);
  [dock expand];removed=nil;start=NSMakePoint(NSMidX(dock.window.frame),NSMinY(dock.window.frame)+100);
  [dock dragTask:@"one" phase:0 point:start];[dock dragTask:@"one" phase:2 point:NSMakePoint(start.x,start.y+90)];assert([removed isEqual:@"one"] && !opened);
  [dock expand];[dock dragTask:@"two" phase:0 point:start];[dock cancelDrag];assert(!dock.dragging && dock.dragPreview==nil);
  [dock collapse];
  // Exercise the actual button mouse responder, including Escape cancellation.
  [dock collapse];[dock setTasks:@[task(@"one",@"working"),task(@"two",@"completed")]];[dock expand];opened=NO;
  BotTaskButton *responder=dock.buttons[0];NSPoint local=[responder convertPoint:NSMakePoint(50,100) toView:nil];
  NSEvent *down=[NSEvent mouseEventWithType:NSEventTypeLeftMouseDown location:local modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:4 clickCount:1 pressure:1];
  [responder mouseDown:down];
  NSEvent *dragged=[NSEvent mouseEventWithType:NSEventTypeLeftMouseDragged location:NSMakePoint(local.x+16,local.y) modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:5 clickCount:1 pressure:1];
  [responder mouseDragged:dragged];assert(dock.dragging);[dock cancelDrag];
  NSEvent *up=[NSEvent mouseEventWithType:NSEventTypeLeftMouseUp location:local modifierFlags:0 timestamp:0 windowNumber:dock.window.windowNumber context:nil eventNumber:6 clickCount:1 pressure:0];
  [responder mouseUp:up];assert(!opened);
  [responder mouseDown:down];[responder mouseUp:up];assert(opened);opened=NO;
  [responder rightMouseDown:down];assert(!opened && responder.menu==nil);
  [dock collapse];
  // New work appears even while the old pointer order is frozen.
  [dock setTasks:@[task(@"one",@"working"),task(@"two",@"completed")]];[dock expand];
  [dock setTasks:@[task(@"three",@"working"),task(@"two",@"completed"),task(@"one",@"working")]];
  assert(dock.buttons.count==3 && [dock.tasks[0][@"id"] isEqual:@"one"] && [dock.tasks[2][@"id"] isEqual:@"three"]);
  [dock.snapshots setObject:@{@"image":[NSImage new],@"time":@"Snapshot"} forKey:@"one"];
  [dock setTasks:@[task(@"two",@"completed"),task(@"three",@"working")]];
  assert(![dock.snapshots objectForKey:@"one"] && !dock.buttons[0].enabled);
  [dock collapse];
  // No scroll container or hidden tail. Every dense card remains reachable in
  // both directions through its actual exposed edge, with stable hit frames.
  NSMutableArray *many=[NSMutableArray new];
  for(int i=0;i<12;i++)[many addObject:task([NSString stringWithFormat:@"task-%d",i],@"completed")];
  [dock setTasks:many];[dock expand];
  assert(dock.buttons.count==12 && dock.window.frame.size.width<=760);
  checkDenseStack(dock);
  BotTaskButton *oldButton=dock.buttons[5];[oldButton mouseEntered:[NSEvent new]];
  [dock setOpening:@"task-5" message:@"Waiting for terminal confirmation"];
  assert(!notice.length && noticePending); // Quick launches must not flash a consent bubble.
  NSTimer *openingTimer=dock.openingTimer;[openingTimer fire];
  assert(noticePending && [notice isEqual:@"Waiting for terminal confirmation"]);
  assert(dock.buttons[5].prominent && dock.buttons[5].hovered && !dock.buttons[0].prominent);
  assert(dock.buttons[5].enabled);
  [dock setOpening:@"" message:@""];
  assert(!notice.length && noticePending && !openingTimer.valid);
  assert(dock.buttons[5].prominent && dock.buttons[5].hovered && dock.buttons[5].enabled);
  assert(oldButton==dock.buttons[5]); // Pending/ready must not replace the mouse responder.
  for(int repeat=0;repeat<8;repeat++) {
    opened=NO;[oldButton mouseDown:down];[oldButton mouseUp:up];assert(opened);
    [dock setOpening:@"task-5" message:@""];
    assert(!notice.length && !dock.openingTimer && [oldButton.statusText isEqual:@"切换窗口…"]);
    opened=NO;[oldButton mouseDown:down]; // Busy input updates the final destination.
    [dock setOpening:@"" message:@""];
    [oldButton mouseUp:up];assert(opened && oldButton==dock.buttons[5]);
  }
  [dock setTransitions:@{@"task-5":@"showing",@"task-11":@"minimizing"}];
  assert(dock.buttons[5].loading && dock.buttons[11].loading);
  assert(dock.buttons[5].enabled && dock.buttons[11].enabled && dock.buttons[0].enabled);
  [dock setTransitions:@{@"task-11":@"minimizing"}];
  assert(!dock.buttons[5].loading && dock.buttons[11].loading);
  [dock setTransitions:@{}];
  [dock render];
  opened=NO;[dock open:oldButton];assert(!opened); // Ignore mouse-up from a replaced view.
  [dock open:dock.buttons[5]];assert(opened);opened=NO;
  [dock setOpening:@"task-11" message:@"Waiting for terminal confirmation"];
  assert(dock.buttons.lastObject.loading && dock.buttons.lastObject.enabled);
  [dock collapse];
  assert(dock.buttons[0].loading && dock.buttons[0].menu==nil);
  __block BOOL canceled=NO;dock.cancelOpening=^{canceled=YES;};[dock cancelOpen];assert(canceled);
  [dock setOpening:@"" message:@""];
  assert(!dock.buttons[0].loading && !notice.length);
  assert(![dock.buttons[0].dots[0] animationForKey:@"pulse"] && !dock.buttons[0].progress.hidden);
  // A recent still skips capture even for an invalid source and keeps the image.
  FixtureCapture *capture=[FixtureCapture new];dock.snapshotCapture=capture;
  [dock.snapshots setObject:@{@"image":[NSImage new],@"time":@"Snapshot",@"created":NSDate.date} forKey:@"task-11"];
  [dock captureTask:@"task-11" source:@{}];assert(capture.calls==0);
  [dock.snapshots setObject:@{@"image":[NSImage new],@"time":@"Snapshot",@"created":[NSDate dateWithTimeIntervalSinceNow:-31]} forKey:@"task-11"];
  [dock captureTask:@"task-11" source:@{}];assert(capture.calls==1 && [dock.snapshots objectForKey:@"task-11"]);
  [dock setTransitions:@{@"task-11":@"showing"}];
  [dock captureTask:@"task-11" source:@{}];assert(capture.calls==1);
  [dock setTransitions:@{}];
  [dock showFailure:@"Opening was not confirmed"];
  assert(!noticePending && [notice isEqual:@"Opening was not confirmed"]);
  [dock setOpening:@"task-11" message:@""];
  assert(!notice.length && noticePending); // Clears only pending hints; renderer owns feedback expiry.
  [dock setOpening:@"" message:@""];
  [dock placeWithPet:NSMakeRect(400,300,240,240) bounds:NSMakeRect(0,0,1200,900) visible:NO];
  [dock setShortcutHeld:YES]; assert(dock.expanded && dock.window.visible);
  [dock placeWithPet:NSMakeRect(400,300,240,240) bounds:NSMakeRect(0,0,1200,900) visible:NO];
  assert(dock.expanded && dock.window.visible);
  [dock setShortcutHeld:NO]; [dock collapse];
  [dock placeWithPet:NSZeroRect bounds:NSZeroRect visible:NO];
  assert(!dock.window.visible && !dock.buttons[0].loading && ![dock.buttons[0].dots[0] animationForKey:@"pulse"]);
  // Bottom edge: a vertical stack grows upwards, with task zero near the feet.
  [dock setTasks:many];[dock placeWithPet:NSMakeRect(0,0,240,240) bounds:NSMakeRect(0,0,1200,900) visible:YES];[dock expand];
  assert(dock.stackVertical && dock.stackUp && dock.window.frame.size.width==240);
  assert(NSMinY(dock.buttons[0].frame)<NSMinY(dock.buttons[1].frame));
  checkDenseStack(dock);
  moved=nil;removed=nil;
  NSPoint verticalStart=NSMakePoint(NSMidX(dock.window.frame),NSMinY(dock.window.frame)+90);
  [dock dragTask:@"task-0" phase:0 point:verticalStart];
  NSPoint inside=NSMakePoint(verticalStart.x,NSMinY(dock.window.frame)+340);
  assert([dock dragActionAt:inside]==2);
  [dock dragTask:@"task-0" phase:2 point:inside];assert([moved isEqual:@"task-0"] && removed==nil);
  verticalStart=NSMakePoint(NSMidX(dock.window.frame),NSMaxY(dock.window.frame)-15);
  [dock dragTask:@"task-1" phase:0 point:verticalStart];[dock cancelDrag];
  // Top edge reverses growth and exposes the first task, not the list tail.
  [dock collapse];[dock placeWithPet:NSMakeRect(960,660,240,240) bounds:NSMakeRect(0,0,1200,900) visible:YES];[dock expand];
  assert(dock.stackVertical && !dock.stackUp);checkDenseStack(dock);
  assert(NSMinY(dock.buttons[0].frame)>NSMinY(dock.buttons[1].frame));
  // Screen/anchor changes cancel a drag before changing orientation.
  verticalStart=NSMakePoint(NSMidX(dock.window.frame),NSMaxY(dock.window.frame)-90);
  [dock dragTask:dock.tasks[0][@"id"] phase:0 point:verticalStart];
  [dock placeWithPet:NSMakeRect(480,450,240,240) bounds:NSMakeRect(0,0,1200,900) visible:YES];
  assert(!dock.dragging && !dock.stackVertical);
  // Capacity is independent of surface size; exercise both axes and growth
  // directions at 30 and 100 cards, including a smaller work area.
  NSRect areas[]={NSMakeRect(0,0,1200,900),NSMakeRect(-900,-600,800,600)};
  for(NSUInteger a=0;a<2;a++)for(NSUInteger corner=0;corner<3;corner++)for(NSUInteger count=30;count<=100;count+=70) {
    [dock collapse];[many removeAllObjects];
    for(NSUInteger i=0;i<count;i++)[many addObject:task([NSString stringWithFormat:@"dense-%lu",(unsigned long)i],@"working")];
    NSRect area=areas[a];
    NSRect pet=NSMakeRect(NSMidX(area)-120,NSMidY(area),240,240);
    if(corner==1)pet.origin=area.origin;
    if(corner==2)pet.origin=NSMakePoint(NSMaxX(area)-240,NSMaxY(area)-240);
    [dock setTasks:many];[dock placeWithPet:pet bounds:area visible:YES];[dock expand];
    assert(dock.buttons.count==count && NSContainsRect(NSInsetRect(area,8,8),dock.window.frame));
    checkDenseStack(dock);
    // Reordering can reach an interior slot, not merely first/last, without scrolling.
    BotTaskButton *target=dock.buttons[count/2];
    NSRect rect=[target.window convertRectToScreen:[target convertRect:target.bounds toView:nil]];
    NSPoint point=NSMakePoint(NSMidX(rect)-0.1,NSMidY(rect)+(dock.stackUp?-0.1:0.1));
    NSString *expected=dock.tasks[count/2][@"id"],*last=dock.tasks.lastObject[@"id"];
    moved=nil;beforeID=nil;
    [dock dragTask:last phase:0 point:point];[dock dragTask:last phase:2 point:point];
    assert([moved isEqual:last] && [beforeID isEqual:expected]);
  }
  [dock stop];
  puts("Adaptive bounds/negative-origin screens, dense stacks (12/30/100), bidirectional hover, controls, no scroll containers, vertical reorder and cancellation passed. Task dock: dots, stable frames, status, pointer order, close/swipe locks, desktop drop, cache cleanup and held shortcut passed.");
 }
 return 0;
}
OBJC
clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa -framework QuartzCore -weak_framework ScreenCaptureKit "$directory/main.m" "$BOT_ROOT/internal/desktop/permission_capture_darwin.m" -o "$directory/fixture"
if [[ "${BOT_TASK_DOCK_LIVE:-}" == 1 ]]; then
  bundle="$BOT_ROOT/dist/Caelis Task Preview.app"
  # Replace only our disposable preview process, never the daily Bot or terminals.
  for preview_pid in $(pgrep -x fixture || true); do
    if [[ "$(ps -ww -p "$preview_pid" -o comm=)" == "$bundle/Contents/MacOS/fixture" ]]; then
      kill -TERM "$preview_pid"
    fi
  done
  mkdir -p "$bundle/Contents/MacOS" "$bundle/Contents/Resources"
  clang -fobjc-arc -fblocks -I "$BOT_ROOT/internal/desktop" -framework Cocoa -framework QuartzCore -weak_framework ScreenCaptureKit "$BOT_ROOT/script/task-dock-preview.m" "$BOT_ROOT/internal/desktop/permission_capture_darwin.m" -o "$bundle/Contents/MacOS/fixture"
  cp "$BOT_ROOT/internal/i18n/locales/zh-CN/native.json" "$bundle/Contents/Resources/native.json"
  cat > "$bundle/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleIdentifier</key><string>dev.caelis.task-preview</string><key>CFBundleName</key><string>Caelis Task Preview</string><key>CFBundlePackageType</key><string>APPL</string><key>NSScreenCaptureUsageDescription</key><string>Cache a local task-window preview. No continuous recording.</string><key>LSUIElement</key><true/></dict></plist>
PLIST
  codesign --force --sign - "$bundle"
  printf '%s\n' "$bundle"
  /usr/bin/open "$bundle"
else
  "$directory/fixture"
fi
