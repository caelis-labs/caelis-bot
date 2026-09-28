//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import "observe_darwin.h"

char *bot_observation_image(uint32_t displayID,int width,int height) {
    if(NSThread.isMainThread||width<1||height<1)return strdup("capture_unavailable");
    if(!CGPreflightScreenCaptureAccess())return strdup("screen_permission_required");
    if(@available(macOS 14.0,*)) {
        dispatch_semaphore_t done=dispatch_semaphore_create(0);
        __block NSString *encoded=nil;
        [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content,NSError *error) {
            SCDisplay *display=nil;
            for(SCDisplay *candidate in content.displays)if(candidate.displayID==displayID){display=candidate;break;}
            if(error||!display){dispatch_semaphore_signal(done);return;}
            SCContentFilter *filter=[[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
            SCStreamConfiguration *config=[SCStreamConfiguration new];
            // Fit the inline transport budget. Never infer pixel scale from DPI.
            double scale=MIN(1.0,1280.0/MAX(width,height));
            config.width=MAX(1,llround(width*scale));config.height=MAX(1,llround(height*scale));config.showsCursor=NO;
            [SCScreenshotManager captureImageWithFilter:filter configuration:config completionHandler:^(CGImageRef image,NSError *failure) {
                if(image&&!failure) {
                    NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithCGImage:image];
                    for(NSNumber *quality in @[@0.8,@0.6,@0.4,@0.25]) {
                        NSData *data=[bitmap representationUsingType:NSBitmapImageFileTypeJPEG properties:@{NSImageCompressionFactor:quality}];
                        if(data.length>0&&data.length<=256*1024){encoded=[data base64EncodedStringWithOptions:0];break;}
                    }
                }
                dispatch_semaphore_signal(done);
            }];
        }];
        if(dispatch_semaphore_wait(done,dispatch_time(DISPATCH_TIME_NOW,5*NSEC_PER_SEC))!=0)return strdup("capture_timeout");
        return strdup(encoded.UTF8String?:"capture_failed");
    }
    return strdup("macos_14_required");
}
