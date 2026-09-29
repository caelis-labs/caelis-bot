//go:build darwin && cgo

#import "bubble_hover_darwin.h"

@implementation BotBubbleSurface {
    NSTrackingArea *_hoverTracking;
    BOOL _hovered;
}
- (BOOL)hovered { return _hovered; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if (_hoverTracking) [self removeTrackingArea:_hoverTracking];
    _hoverTracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect
        options:NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect
        owner:self userInfo:nil];
    [self addTrackingArea:_hoverTracking];
}
- (void)setHovered:(BOOL)inside {
    if (_hovered == inside) return;
    _hovered = inside;
    if (self.hoverChanged) self.hoverChanged(inside);
}
- (void)mouseEntered:(NSEvent *)event {
    if (self.window.visible && self.window.onActiveSpace && !self.hidden) [self setHovered:YES];
}
- (void)mouseExited:(NSEvent *)event { [self setHovered:NO]; }
- (void)resetHover { [self setHovered:NO]; }
- (void)viewWillMoveToWindow:(NSWindow *)window {
    [self resetHover];
    [super viewWillMoveToWindow:window];
}
@end
