#import <Cocoa/Cocoa.h>
// One still after an explicit terminal action, only with existing screen access.
// No permission prompt, picker, stream, timer, transcript or model access.
@interface BotTaskSnapshot : NSObject
- (void)capture:(NSDictionary *)source completion:(void (^)(NSImage *image))completion;
- (void)cancel;
@end
