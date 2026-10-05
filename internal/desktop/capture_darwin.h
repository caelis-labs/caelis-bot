#include <stdint.h>
#ifdef __OBJC__
#import <Cocoa/Cocoa.h>

// Native presentation and transient edits stay here; Go owns submitted bytes,
// model capability, request identities and durable recovery.
@interface BotCapture : NSObject
@property(nonatomic) uintptr_t handle;
@property(nonatomic,copy) NSString *root;
@property(nonatomic,copy) NSDictionary *language;
@property(nonatomic,copy) NSArray<NSWindow *> *excludedWindows;
@property(nonatomic) BOOL includeBackground;
@property(nonatomic) BOOL enabled;
- (void)capture;
- (void)paste;
- (void)togglePins;
- (void)setAvailability:(NSDictionary *)state;
- (void)receipt:(NSDictionary *)result;
- (void)restore:(NSArray *)records;
- (void)stop;
@end
#endif

void *bot_capture_create(uintptr_t handle,const char *root);
void bot_capture_command(void *capture,int command);
void bot_capture_availability(void *capture,const char *json);
void bot_capture_receipt(void *capture,const char *json);
void bot_capture_restore(void *capture,const char *json);
void bot_capture_stop(void *capture);
void bot_capture_preferences(void *capture,int includeBackground);
int bot_capture_copy_image(const void *bytes,int length);
void bot_bind_capture(void *host,void *capture);
