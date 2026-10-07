#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>
#import <ServiceManagement/ServiceManagement.h>
#include <string.h>
#include <stdatomic.h>
#include "login_at_login_darwin.h"

static atomic_bool bot_login_launch = false;
static id bot_login_observer = nil;

static BOOL bot_login_bundle_matches(const char *expected_id) {
    if (!expected_id) return NO;
    NSString *expected = [NSString stringWithUTF8String:expected_id];
    NSString *actual = NSBundle.mainBundle.bundleIdentifier;
    NSString *path = NSBundle.mainBundle.bundlePath;
    return [actual isEqualToString:expected] && [path.pathExtension isEqualToString:@"app"];
}

int bot_login_item_status(const char *expected_id) {
    @autoreleasepool {
        if (@available(macOS 13.0, *)) {
            if (!bot_login_bundle_matches(expected_id)) return 3;
            switch (SMAppService.mainAppService.status) {
                case SMAppServiceStatusEnabled: return 1;
                case SMAppServiceStatusRequiresApproval: return 2;
                case SMAppServiceStatusNotRegistered:
                case SMAppServiceStatusNotFound: return 0;
                default: return 3;
            }
        }
        return -1;
    }
}

int bot_login_item_set(const char *expected_id, int enabled, char **error_message) {
    @autoreleasepool {
        if (error_message) *error_message = NULL;
        if (@available(macOS 13.0, *)) {
            if (!bot_login_bundle_matches(expected_id)) return 0;
            NSError *error = nil;
            BOOL ok = enabled ? [SMAppService.mainAppService registerAndReturnError:&error]
                              : [SMAppService.mainAppService unregisterAndReturnError:&error];
            if (!ok && error_message) {
                const char *message = error.localizedDescription.UTF8String;
                *error_message = strdup(message ?: "The login item could not be changed");
            }
            return ok ? 1 : 0;
        }
        return 0;
    }
}

int bot_login_item_open_settings(void) {
    @autoreleasepool {
        if (@available(macOS 13.0, *)) {
            [SMAppService openSystemSettingsLoginItems];
            return 1;
        }
        return 0;
    }
}

void bot_track_login_launch(void) {
    bot_login_observer = [NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidFinishLaunchingNotification
        object:nil queue:nil usingBlock:^(__unused NSNotification *note) {
            NSAppleEventDescriptor *event = NSAppleEventManager.sharedAppleEventManager.currentAppleEvent;
            BOOL atLogin = event.eventClass == kCoreEventClass && event.eventID == kAEOpenApplication &&
                [event paramDescriptorForKeyword:keyAEPropData].enumCodeValue == keyAELaunchedAsLogInItem;
            atomic_store(&bot_login_launch, atLogin);
        }];
}

int bot_launched_at_login(void) {
    return atomic_load(&bot_login_launch);
}
