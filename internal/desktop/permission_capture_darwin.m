//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import "permission_capture_darwin.h"
#include <stdatomic.h>

// A framework refusal is stronger evidence than a stale CG preflight result.
// Only a successful explicit permission request (or process restart) clears it.
static atomic_bool screenCaptureDenied=false;
int bot_screen_capture_denied(void) { return atomic_load(&screenCaptureDenied); }
void bot_screen_capture_set_denied(int denied) { atomic_store(&screenCaptureDenied,denied!=0); }

@interface BotScreenPermissionRequest : NSObject
@property int status;
@property long error;
@end
@implementation BotScreenPermissionRequest
@end

void *bot_screen_permission_request(void) {
    BotScreenPermissionRequest *state=[BotScreenPermissionRequest new];
    if(@available(macOS 14.0,*)) {
        // Explicit Settings action only. Use the same framework as task previews
        // to register this running app with TCC. Legacy CGRequestScreenCaptureAccess
        // may return false without registering a request on newer systems.
        // Discard all returned metadata; do not create a filter, image or stream.
        [SCShareableContent getShareableContentExcludingDesktopWindows:YES onScreenWindowsOnly:YES
            completionHandler:^(SCShareableContent *content,NSError *error) {
                @synchronized(state) {
                    state.error=error.code;
                    state.status=!error&&content ? 1 :
                        ([error.domain isEqualToString:SCStreamErrorDomain] && error.code==SCStreamErrorUserDeclined ? 2 : 3);
                    if(state.status==1)bot_screen_capture_set_denied(0);
                    else if(state.status==2)bot_screen_capture_set_denied(1);
                }
            }];
    } else state.status=4;
    return (__bridge_retained void *)state;
}
int bot_screen_permission_poll(void *handle,long *error) {
    BotScreenPermissionRequest *state=(__bridge BotScreenPermissionRequest *)handle;
    @synchronized(state) { *error=state.error;return state.status; }
}
void bot_screen_permission_release(void *handle) {
    // The completion block retains state if the bridge caller cancels or times out.
    // A late response cannot open Settings, capture pixels or mutate UI state.
    __unused BotScreenPermissionRequest *state=(__bridge_transfer BotScreenPermissionRequest *)handle;
}
