#import <Cocoa/Cocoa.h>

// A local presentation of host-owned tasks. No runtime or task lifecycle lives
// in these windows; only an explicit click can request a terminal attachment.
@interface BotTaskDock : NSObject
@property(copy) void (^openTask)(NSString *identifier);
@property(copy) void (^unpinTask)(NSString *identifier);
@property(copy) void (^lockTask)(NSString *identifier, BOOL locked);
@property(copy) void (^reorderTask)(NSString *identifier, NSString *before);
@property(copy) void (^placeTask)(NSString *identifier, NSPoint point);
@property(copy) void (^cancelOpening)(void);
@property(copy) void (^gesture)(NSString *name);
// Reuse the host's message bubble; the dock does not own a notice window.
@property(copy) void (^notice)(NSString *message, BOOL pending);
@property(readonly) NSPanel *window;
@property(readonly) NSUInteger count;
@property(copy, nonatomic) NSDictionary<NSString *, NSString *> *language;
// A temporary attachment menu keeps the entry clickable, but only a click
// expands it; opening the menu collapses any cards already on screen.
- (void)captureTask:(NSString *)identifier source:(NSDictionary *)source;
- (void)setTasks:(NSArray *)tasks;
- (void)placeWithPet:(NSRect)pet bounds:(NSRect)bounds visible:(BOOL)visible;
- (void)showFailure:(NSString *)message;
- (void)setTransitions:(NSDictionary *)transitions;
- (void)setOpening:(NSString *)identifier message:(NSString *)message;
- (void)toggle;
- (void)setShortcutHeld:(BOOL)held;
- (void)stop;
@end
