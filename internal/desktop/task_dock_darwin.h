#import <Cocoa/Cocoa.h>

// A local presentation of host-owned tasks. No runtime or task lifecycle lives
// in these windows; only an explicit click can request a terminal attachment.
@interface BotTaskDock : NSObject
@property(copy) void (^openTask)(NSString *identifier);
@property(copy) void (^gesture)(NSString *name);
@property(readonly) NSPanel *window;
@property(readonly) NSUInteger count;
- (void)setTasks:(NSArray *)tasks;
- (void)placeWithPet:(NSRect)pet bounds:(NSRect)bounds visible:(BOOL)visible;
- (void)showFailure:(NSString *)message;
- (void)stop;
@end
