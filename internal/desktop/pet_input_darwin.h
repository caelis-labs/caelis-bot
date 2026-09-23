#import <Cocoa/Cocoa.h>

// Physical pet input has one native owner. The renderer does not interpret clicks.
@interface BotPetInputView : NSView <NSGestureRecognizerDelegate>
@property(nonatomic, copy) void (^singleClick)(void);
@property(nonatomic, copy) void (^doubleClick)(void);
@property(nonatomic, copy) void (^beginDrag)(NSEvent *down);
@property(nonatomic, copy) void (^pointerChanged)(void);
@property(nonatomic, copy) NSMenu *(^contextMenu)(void);
- (void)cancelInteraction;
- (BOOL)capturesPointer:(NSPoint)screenPoint atTime:(NSTimeInterval)time;
@end
