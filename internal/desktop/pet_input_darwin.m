//go:build darwin && cgo

#import "pet_input_darwin.h"

@interface BotPetInputView ()
@property NSClickGestureRecognizer *single;
@property NSClickGestureRecognizer *doubleTap;
@property NSPanGestureRecognizer *pan;
@property NSEvent *press;
@property NSPoint pressPoint;
@property NSTimeInterval captureUntil;
@property BOOL acceptingClick;
@end

@implementation BotPetInputView
- (instancetype)initWithFrame:(NSRect)frame {
    if ((self = [super initWithFrame:frame])) {
        _single = [[NSClickGestureRecognizer alloc] initWithTarget:self action:@selector(recognized:)];
        _doubleTap = [[NSClickGestureRecognizer alloc] initWithTarget:self action:@selector(recognized:)];
        _single.numberOfClicksRequired = 1;
        _doubleTap.numberOfClicksRequired = 2;
        _pan = [[NSPanGestureRecognizer alloc] initWithTarget:self action:@selector(panRecognized:)];
        for (NSClickGestureRecognizer *click in @[_single, _doubleTap]) {
            click.buttonMask = 1;
            click.delegate = self;
            // Let the view see ordinary down/drag/up events without a private
            // tracking loop. AppKit still exclusively arbitrates the clicks.
            click.delaysPrimaryMouseButtonEvents = NO;
            [self addGestureRecognizer:click];
        }
        _pan.buttonMask = 1;
        _pan.delaysPrimaryMouseButtonEvents = NO;
        _pan.delegate = self;
        [self addGestureRecognizer:_pan];
    }
    return self;
}
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (BOOL)gestureRecognizer:(NSGestureRecognizer *)gesture shouldRequireFailureOfGestureRecognizer:(NSGestureRecognizer *)other {
    // Only single waits for double to fail. Double never depends on a single
    // callback, renderer state, or a previously opened composer.
    return (gesture == self.single && other == self.doubleTap) ||
        ((gesture == self.single || gesture == self.doubleTap) && other == self.pan);
}
- (BOOL)gestureRecognizer:(NSGestureRecognizer *)gesture shouldAttemptToRecognizeWithEvent:(NSEvent *)event {
    // Capture the original down before AppKit arbitrates/forwards view events.
    // Window Server explicitly requires that original event for window dragging.
    if (event.type == NSEventTypeLeftMouseDown) [self mouseDown:event];
    return YES;
}
- (void)recognized:(NSClickGestureRecognizer *)gesture {
    if (!self.acceptingClick || gesture.state != NSGestureRecognizerStateRecognized) return;
    self.acceptingClick = NO;
    self.press = nil;
    self.captureUntil = 0;
    if (self.pointerChanged) self.pointerChanged();
    if (gesture == self.doubleTap) {
        if (self.doubleClick) self.doubleClick();
    } else if (self.singleClick) self.singleClick();
}
- (void)mouseDown:(NSEvent *)event {
    self.press = event;
    self.pressPoint = [self.window convertPointToScreen:event.locationInWindow];
    self.acceptingClick = YES;
    self.captureUntil = 0;
    if (self.pointerChanged) self.pointerChanged();
}
- (void)mouseUp:(NSEvent *)event {
    self.press = nil;
    // Keep the original small hit region between the two releases, even if
    // animated hair or a blink changes the silhouette. This is only capture;
    // recognition/timing belongs to NSClickGestureRecognizer, not this lease.
    if (self.acceptingClick) self.captureUntil = event.timestamp + NSEvent.doubleClickInterval;
    if (self.pointerChanged) self.pointerChanged();
}
- (void)panRecognized:(NSPanGestureRecognizer *)gesture {
    if (gesture.state != NSGestureRecognizerStateBegan || !self.press) return;
    NSEvent *down = self.press;
    [self cancelInteraction];
    if (self.beginDrag) self.beginDrag(down);
}
- (BOOL)capturesPointer:(NSPoint)point atTime:(NSTimeInterval)time {
    if (self.press) return YES;
    return self.acceptingClick && time <= self.captureUntil &&
        hypot(point.x-self.pressPoint.x,point.y-self.pressPoint.y) <= 8;
}
- (void)cancelInteraction {
    self.acceptingClick = NO;
    self.press = nil;
    self.captureUntil = 0;
    // AppKit cancellation discards pending single/double recognition. A late
    // action also checks acceptingClick before it can cause a product action.
    for (NSGestureRecognizer *gesture in @[self.single,self.doubleTap,self.pan]) gesture.enabled = NO;
    for (NSGestureRecognizer *gesture in @[self.single,self.doubleTap,self.pan]) gesture.enabled = YES;
    if (self.pointerChanged) self.pointerChanged();
}
- (BOOL)accessibilityPerformPress {
    [self cancelInteraction];
    if (self.singleClick) self.singleClick();
    return YES;
}
- (BOOL)accessibilityPerformShowMenu {
    [self cancelInteraction];
    return [self.contextMenu() popUpMenuPositioningItem:nil atLocation:NSMakePoint(NSMidX(self.bounds),NSMidY(self.bounds)) inView:self];
}
- (void)rightMouseDown:(NSEvent *)event {
    [self cancelInteraction];
    [NSMenu popUpContextMenu:self.contextMenu() withEvent:event forView:self];
}
@end
