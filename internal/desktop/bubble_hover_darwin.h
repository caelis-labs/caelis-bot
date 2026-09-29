#import <Cocoa/Cocoa.h>

// Passive reading must work even while another app owns keyboard focus.
@interface BotBubbleSurface : NSView
@property (copy) void (^hoverChanged)(BOOL inside);
@property (readonly) BOOL hovered;
- (void)resetHover;
@end
