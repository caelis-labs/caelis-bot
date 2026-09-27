//go:build darwin && cgo
#import "capture_image_darwin.h"
#include <math.h>

// Match screeninput's UTF-8 limits. Optional OS metadata may be shortened;
// user-authored notes must remain intact for correction instead of truncation.
static NSString *boundedUTF8(NSString *value, NSUInteger maximum) {
    NSData *data=[(value?:@"") dataUsingEncoding:NSUTF8StringEncoding];
    if(data.length<=maximum)return value?:@"";
    while(maximum>0) {
        NSString *prefix=[[NSString alloc] initWithData:[data subdataWithRange:NSMakeRange(0,maximum)] encoding:NSUTF8StringEncoding];
        if(prefix)return prefix;
        maximum--;
    }
    return @"";
}

static NSRect markRect(NSDictionary *mark) {
    NSPoint a=[mark[@"a"] pointValue],b=[mark[@"b"] pointValue];
    return NSMakeRect(MIN(a.x,b.x),MIN(a.y,b.y),fabs(a.x-b.x),fabs(a.y-b.y));
}
static NSBitmapImageRep *bitmap(NSInteger width,NSInteger height) {
    return [[NSBitmapImageRep alloc] initWithBitmapDataPlanes:NULL pixelsWide:MAX(1,width) pixelsHigh:MAX(1,height)
        bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSCalibratedRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
}
@implementation BotCaptureDocument
- (instancetype)init {
    if((self=[super init])) {
        _marks=[NSMutableArray new];_redoMarks=[NSMutableArray new];_windows=@[];
        _source=@"screen";_note=@"";_applicationName=@"";_windowTitle=@"";
        _capturedAt=[[NSISO8601DateFormatter new] stringFromDate:NSDate.date];
        _includeBackground=YES;_pixelScale=1;_outcome=@"draft";
    }
    return self;
}
- (void)addMark:(NSDictionary *)mark { [self.marks addObject:mark];[self.redoMarks removeAllObjects];self.identifier=nil; }
- (void)undo {
    if(!self.marks.count)return;
    [self.redoMarks addObject:self.marks.lastObject];[self.marks removeLastObject];self.identifier=nil;
}
- (void)redo {
    if(!self.redoMarks.count)return;
    [self.marks addObject:self.redoMarks.lastObject];[self.redoMarks removeLastObject];self.identifier=nil;
}
- (void)drawMarks {
    // Opaque privacy masks are always last, including after overlapping mosaic.
    NSArray *ordered=[self.marks sortedArrayUsingComparator:^NSComparisonResult(NSDictionary *a,NSDictionary *b) {
        BOOL ar=[a[@"kind"] isEqual:@"redact"],br=[b[@"kind"] isEqual:@"redact"];
        return ar==br?NSOrderedSame:(ar?NSOrderedDescending:NSOrderedAscending);
    }];
    for(NSDictionary *mark in ordered) {
        NSString *kind=mark[@"kind"];
        NSPoint a=[mark[@"a"] pointValue],b=[mark[@"b"] pointValue];
        NSRect r=markRect(mark);
        CGFloat width=[mark[@"width"] doubleValue];
        NSColor *color=mark[@"color"]?:NSColor.systemRedColor;
        [color setStroke];[color setFill];
        if([kind isEqual:@"text"]) {
            NSString *text=mark[@"text"]?:@"";
            [text drawAtPoint:a withAttributes:@{NSFontAttributeName:[NSFont systemFontOfSize:MAX(14,width*5) weight:NSFontWeightSemibold],NSForegroundColorAttributeName:color,NSBackgroundColorAttributeName:[NSColor.textBackgroundColor colorWithAlphaComponent:0.86]}];
        } else if([kind isEqual:@"redact"]) {
            [NSColor.blackColor setFill];NSRectFill(NSInsetRect(r,-1,-1));
        } else if([kind isEqual:@"mosaic"]) {
            // Downsample the source region, then nearest-neighbour enlarge.
            NSInteger w=MAX(1,ceil(r.size.width/12)),h=MAX(1,ceil(r.size.height/12));
            NSBitmapImageRep *rep=bitmap(w,h);
            NSGraphicsContext *old=NSGraphicsContext.currentContext;
            NSGraphicsContext *small=[NSGraphicsContext graphicsContextWithBitmapImageRep:rep];
            NSGraphicsContext.currentContext=small;
            // NSImage source rectangles are bottom-left even in a flipped view.
            NSRect source=NSMakeRect(r.origin.x,self.canvasSize.height-NSMaxY(r),r.size.width,r.size.height);
            [self.image drawInRect:NSMakeRect(0,0,w,h) fromRect:source operation:NSCompositingOperationCopy fraction:1 respectFlipped:NO hints:nil];
            NSGraphicsContext.currentContext=old;
            NSImage *pixelated=[[NSImage alloc] initWithSize:NSMakeSize(w,h)];[pixelated addRepresentation:rep];
            NSImageInterpolation previous=old.imageInterpolation;old.imageInterpolation=NSImageInterpolationNone;
            [pixelated drawInRect:NSInsetRect(r,-0.5,-0.5) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
            old.imageInterpolation=previous;
        } else {
            NSBezierPath *path=[NSBezierPath new];path.lineWidth=width;path.lineCapStyle=NSLineCapStyleRound;path.lineJoinStyle=NSLineJoinStyleRound;
            if([kind isEqual:@"rectangle"]) [path appendBezierPathWithRect:r];
            else {
                [path moveToPoint:a];[path lineToPoint:b];
                CGFloat angle=atan2(b.y-a.y,b.x-a.x),length=MAX(10,width*4);
                [path moveToPoint:NSMakePoint(b.x-length*cos(angle-0.5),b.y-length*sin(angle-0.5))];
                [path lineToPoint:b];[path lineToPoint:NSMakePoint(b.x-length*cos(angle+0.5),b.y-length*sin(angle+0.5))];
            }
            [path stroke];
        }
    }
}
- (NSBitmapImageRep *)render:(BOOL)background maximumEdge:(CGFloat)edge {
    NSRect region=background?NSMakeRect(0,0,self.canvasSize.width,self.canvasSize.height):self.selection;
    if(region.size.width<=0 || region.size.height<=0)return nil;
    CGFloat scale=self.pixelScale;
    if(edge>0)scale=MIN(scale,edge/MAX(region.size.width,region.size.height));
    NSInteger w=MAX(1,llround(region.size.width*scale)),h=MAX(1,llround(region.size.height*scale));
    NSBitmapImageRep *rep=bitmap(w,h);
    NSGraphicsContext *old=NSGraphicsContext.currentContext;
    NSGraphicsContext *target=[NSGraphicsContext graphicsContextWithBitmapImageRep:rep];
    CGContextRef cg=target.CGContext;
    CGContextTranslateCTM(cg,0,h);CGContextScaleCTM(cg,(CGFloat)w/region.size.width,-(CGFloat)h/region.size.height);
    CGContextTranslateCTM(cg,-region.origin.x,-region.origin.y);
    NSGraphicsContext.currentContext=[NSGraphicsContext graphicsContextWithCGContext:cg flipped:YES];
    [self.image drawInRect:NSMakeRect(0,0,self.canvasSize.width,self.canvasSize.height) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
    [self drawMarks];
    if(background) {
        [NSColor.systemRedColor setStroke];NSBezierPath *outline=[NSBezierPath bezierPathWithRect:NSInsetRect(self.selection,-2,-2)];
        outline.lineWidth=2/scale;[outline stroke];
    }
    NSGraphicsContext.currentContext=old;
    return rep;
}
- (BOOL)noteWithinLimit {
    NSData *data=[(self.note?:@"") dataUsingEncoding:NSUTF8StringEncoding];
    return data && data.length<=4096;
}
- (NSDictionary *)writeToRoot:(NSString *)root error:(NSError **)error {
    if(![self noteWithinLimit]) {
        if(error)*error=[NSError errorWithDomain:@"CaelisCapture" code:1 userInfo:@{NSLocalizedDescriptionKey:@"Note exceeds 4096 UTF-8 bytes"}];
        return nil;
    }
    if(!self.identifier.length)self.identifier=NSUUID.UUID.UUIDString;
    NSString *dir=[root stringByAppendingPathComponent:self.identifier];
    if(![NSFileManager.defaultManager createDirectoryAtPath:dir withIntermediateDirectories:YES attributes:@{NSFilePosixPermissions:@0700} error:error])return nil;
    CGFloat edge=MAX(self.selection.size.width,self.selection.size.height)*self.pixelScale;
    NSData *crop=nil;
    for(int i=0;i<12;i++) {
        crop=[[self render:NO maximumEdge:edge] representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        if(crop.length<=8*1024*1024)break;
        edge*=0.8;
    }
    if(!crop || crop.length>8*1024*1024)return nil;
    if(![crop writeToFile:[dir stringByAppendingPathComponent:@"selection.png"] options:NSDataWritingAtomic error:error])return nil;
    NSMutableDictionary *meta=[@{@"version":@1,@"id":self.identifier,@"capturedAt":self.capturedAt,@"application":boundedUTF8(self.applicationName,512),@"windowTitle":boundedUTF8(self.windowTitle,2048),@"source":self.source,@"note":self.note?:@"",@"background":@NO,@"backgroundWidth":@0,@"backgroundHeight":@0,@"selection":@{@"x":@0,@"y":@0,@"width":@0,@"height":@0}} mutableCopy];
    if(self.includeBackground && [self.source isEqual:@"screen"]) {
        NSBitmapImageRep *rep=[self render:YES maximumEdge:2400];
        NSData *data=[rep representationUsingType:NSBitmapImageFileTypeJPEG properties:@{NSImageCompressionFactor:@0.85}];
        if(!data || data.length>8*1024*1024 || ![data writeToFile:[dir stringByAppendingPathComponent:@"context.jpg"] options:NSDataWritingAtomic error:error])return nil;
        double sx=(double)rep.pixelsWide/self.canvasSize.width,sy=(double)rep.pixelsHigh/self.canvasSize.height;
        NSInteger x=floor(self.selection.origin.x*sx),y=floor(self.selection.origin.y*sy);
        NSInteger right=MIN(rep.pixelsWide,ceil(NSMaxX(self.selection)*sx)),bottom=MIN(rep.pixelsHigh,ceil(NSMaxY(self.selection)*sy));
        meta[@"background"]=@YES;meta[@"backgroundWidth"]=@(rep.pixelsWide);meta[@"backgroundHeight"]=@(rep.pixelsHigh);
        meta[@"selection"]=@{@"x":@(x),@"y":@(y),@"width":@(right-x),@"height":@(bottom-y)};
    }
    NSData *json=[NSJSONSerialization dataWithJSONObject:meta options:0 error:error];
    if(!json || ![json writeToFile:[dir stringByAppendingPathComponent:@"capture.json"] options:NSDataWritingAtomic error:error])return nil;
    for(NSString *file in @[@"selection.png",@"context.jpg",@"capture.json"]) {
        [NSFileManager.defaultManager setAttributes:@{NSFilePosixPermissions:@0600} ofItemAtPath:[dir stringByAppendingPathComponent:file] error:nil];
    }
    return meta;
}
@end
