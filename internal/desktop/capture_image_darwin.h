#import <Cocoa/Cocoa.h>

@interface BotCaptureDocument : NSObject
@property NSImage *image;
@property NSSize canvasSize;
@property CGFloat pixelScale;
@property NSRect selection;
@property NSMutableArray<NSDictionary *> *marks;
@property NSMutableArray<NSDictionary *> *redoMarks;
@property NSArray<NSValue *> *windows;
@property NSString *applicationName;
@property NSString *windowTitle;
@property NSString *capturedAt;
@property NSString *source;
@property NSString *note;
@property BOOL includeBackground;
@property NSString *identifier;
@property NSString *outcome;
- (instancetype)init;
- (void)addMark:(NSDictionary *)mark;
- (void)undo;
- (void)redo;
- (void)drawMarks;
- (NSBitmapImageRep *)render:(BOOL)background maximumEdge:(CGFloat)edge;
- (BOOL)noteWithinLimit;
- (NSDictionary *)writeToRoot:(NSString *)root error:(NSError **)error;
@end
