#import <Cocoa/Cocoa.h>
#import "bubble_hover_darwin.h"
#include <assert.h>
@interface PassiveBubble:NSPanel @end
@implementation PassiveBubble
- (BOOL)canBecomeKeyWindow { return NO; }
@end
int main(void) { @autoreleasepool {
 [NSApplication sharedApplication];
 PassiveBubble *panel=[[PassiveBubble alloc] initWithContentRect:NSMakeRect(100,100,360,68) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
 BotBubbleSurface *surface=[[BotBubbleSurface alloc] initWithFrame:NSMakeRect(0,0,360,68)];panel.contentView=surface;
 NSMutableArray *events=[NSMutableArray new];surface.hoverChanged=^(BOOL inside){[events addObject:@(inside)];};
 [panel orderFrontRegardless];[surface updateTrackingAreas];
 assert(surface.trackingAreas.count==1);
 NSTrackingAreaOptions options=surface.trackingAreas.firstObject.options;
 assert(options&NSTrackingActiveAlways);assert(options&NSTrackingInVisibleRect);
 assert(options&NSTrackingMouseEnteredAndExited);
 [surface mouseEntered:[NSEvent new]];[surface mouseEntered:[NSEvent new]];
 assert(surface.hovered&&events.count==1&&!panel.keyWindow);
 [panel setFrame:NSMakeRect(100,100,360,480) display:YES];[surface updateTrackingAreas];
 assert(surface.trackingAreas.count==1&&surface.hovered);
 [surface mouseExited:[NSEvent new]];assert(!surface.hovered&&events.count==2);
 [surface mouseEntered:[NSEvent new]];[surface resetHover];[panel orderOut:nil];
 [surface mouseEntered:[NSEvent new]];assert(!surface.hovered&&events.count==4);
 [panel orderFrontRegardless];[surface mouseEntered:[NSEvent new]];panel.contentView=[NSView new];
 assert(!surface.hovered&&events.count==6&&!panel.keyWindow);
 [panel orderOut:nil];puts("PASS: passive bubble tracking, resize, exit, hide and detach");
 }return 0; }
