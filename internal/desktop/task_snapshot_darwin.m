//go:build darwin && cgo
#import "task_snapshot_darwin.h"
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import "permission_capture_darwin.h"
#include <libproc.h>
static BOOL taskSnapshotDenied(NSError *error) API_AVAILABLE(macos(12.3)) {
    if([error.domain isEqualToString:SCStreamErrorDomain] && error.code==SCStreamErrorUserDeclined) {
        bot_screen_capture_set_denied(1);
        NSLog(@"Task terminal preview paused: screen access declined");
        return YES;
    }
    return NO;
}
static BOOL taskSnapshotProcessMatches(NSDictionary *source) {
    struct proc_bsdinfo info={0};int pid=[source[@"pid"] intValue];
    if(pid<=0 || proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&info,sizeof(info))!=sizeof(info))return NO;
    return info.pbi_start_tvsec*1000000ULL+info.pbi_start_tvusec==[source[@"birth"] unsignedLongLongValue];
}
static SCWindow *taskSnapshotWindow(NSArray<SCWindow *> *windows, NSDictionary *source, NSUInteger *eligible) API_AVAILABLE(macos(12.3)) {
    SCWindow *selected=nil;*eligible=0;
    for(SCWindow *window in windows) {
        if(window.owningApplication.processID!=[source[@"pid"] intValue] || window.windowLayer!=0 ||
           window.frame.size.width<=80 || window.frame.size.height<=50)continue;
        ++*eligible;selected=window;
    }
    return *eligible==1?selected:nil; // Never guess between multiple candidates.
}
@interface BotTaskSnapshot ()
@property NSUInteger generation;
@end
@implementation BotTaskSnapshot
- (void)cancel {++self.generation;}
- (void)capture:(NSDictionary *)source completion:(void (^)(NSImage *))completion {
    [self cancel];NSUInteger generation=self.generation;
    if(bot_screen_capture_denied()){completion(nil);return;}
    if(!CGPreflightScreenCaptureAccess()){NSLog(@"Task terminal preview skipped: screen permission required");completion(nil);return;}
    if(!taskSnapshotProcessMatches(source)){NSLog(@"Task terminal preview skipped: process changed");completion(nil);return;}
    if(@available(macOS 14.0,*)) {
        // A successful toggle may just have hidden the terminal. Its identity
        // remains valid; restricting enumeration to visible windows loses it.
        [SCShareableContent getShareableContentExcludingDesktopWindows:YES onScreenWindowsOnly:NO completionHandler:^(SCShareableContent *content,NSError *error){
            dispatch_async(dispatch_get_main_queue(), ^{
                if(generation!=self.generation)return;
                if(taskSnapshotDenied(error)){completion(nil);return;}
                NSUInteger eligible=0;SCWindow *selected=taskSnapshotWindow(content.windows,source,&eligible);
                // No guessing between windows of a shared Terminal/iTerm process.
                if(error || !selected || !taskSnapshotProcessMatches(source)){NSLog(@"Task terminal preview skipped: window unavailable or ambiguous (candidates=%lu code=%ld)",(unsigned long)eligible,(long)error.code);completion(nil);return;}
                SCContentFilter *filter=[[SCContentFilter alloc] initWithDesktopIndependentWindow:selected];
                SCStreamConfiguration *config=[SCStreamConfiguration new];
                CGRect rect=selected.frame;CGFloat scale=MIN(640/MAX(1,rect.size.width),400/MAX(1,rect.size.height));
                config.width=MAX(1,rect.size.width*scale);config.height=MAX(1,rect.size.height*scale);config.showsCursor=NO;
                [SCScreenshotManager captureImageWithFilter:filter configuration:config completionHandler:^(CGImageRef image,NSError *error){
                    NSImage *still=image?[[NSImage alloc] initWithCGImage:image size:NSMakeSize(CGImageGetWidth(image),CGImageGetHeight(image))]:nil;
                    dispatch_async(dispatch_get_main_queue(), ^{
                        if(generation!=self.generation)return;
                        if(taskSnapshotDenied(error)){completion(nil);return;}
                        BOOL valid=!error&&still&&taskSnapshotProcessMatches(source);
                        NSLog(@"Task terminal preview %@ (code=%ld)",valid?@"cached":@"unavailable",(long)error.code);
                        completion(valid?still:nil);
                    });
                }];
            });
        }];
    } else completion(nil);
}
@end
