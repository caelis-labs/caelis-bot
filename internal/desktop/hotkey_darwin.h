#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>

// Main-thread-only ownership of the four native shortcut registrations.
@interface BotHotkeys : NSObject
@property(copy) void (^action)(NSUInteger slot, BOOL down);
- (OSStatus)configureKey:(NSString *)key flags:(int)flags enabled:(BOOL)enabled slot:(NSUInteger)slot;
- (void)reconcileReleases;
- (void)invalidate;
@end
