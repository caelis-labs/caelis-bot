#import <Cocoa/Cocoa.h>
#import <CoreServices/CoreServices.h>
#include "native_darwin.h"

static NSImage *botApplicationIcon;
int bot_install_app_icon(unsigned char *bytes, int length) {
    if (!bytes || length <= 0) return 0;
    NSBitmapImageRep *bitmap = [[NSBitmapImageRep alloc] initWithData:[NSData dataWithBytes:bytes length:length]];
    if (!bitmap || bitmap.pixelsWide <= 0 || bitmap.pixelsHigh <= 0) return 0;
    NSImage *image = [[NSImage alloc] initWithSize:NSMakeSize(bitmap.pixelsWide, bitmap.pixelsHigh)];
    [image addRepresentation:bitmap];
    botApplicationIcon = image;
    NSApp.applicationIconImage = image;
    return 1;
}

int bot_window_open(void *pointer) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    // Covered, app-hidden and minimised windows are still open.
    return window.visible || window.miniaturized;
}
int bot_window_visible(void *pointer) {
    return [(__bridge NSWindow *)pointer isVisible];
}
int bot_window_can_hide(void *pointer) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    // Only a foreground chat toggles away. Background chat must be recalled;
    // a native file sheet must remain reachable regardless of focus.
    return NSApp.active && window.keyWindow && window.visible &&
        !window.miniaturized && !window.attachedSheet;
}
void bot_sync_dock(void *history, void *settings, int opening) {
    // Promote before ordering a contextual window in. Never use occlusion:
    // covering or minimising a window must retain its Dock entry.
    NSApplicationActivationPolicy policy = opening || bot_window_open(history) || bot_window_open(settings)
        ? NSApplicationActivationPolicyRegular : NSApplicationActivationPolicyAccessory;
    if (NSApp.activationPolicy != policy) {
        [NSApp setActivationPolicy:policy];
        // Accessory -> regular creates the Dock tile after Wails' startup icon
        // assignment. Reapply the decoded, retained image at that transition.
        if (policy == NSApplicationActivationPolicyRegular && botApplicationIcon)
            NSApp.applicationIconImage = botApplicationIcon;
    }
}

void bot_close_context_window(void *history, void *settings) {
    NSWindow *chat = (__bridge NSWindow *)history;
    NSWindow *preferences = (__bridge NSWindow *)settings;
    NSWindow *target = nil;
    // Dock actions may arrive while another app is active. Native window order
    // also covers app-hidden and minimised windows without reopening either.
    for (NSWindow *window in @[NSApp.keyWindow ?: NSNull.null, NSApp.mainWindow ?: NSNull.null]) {
        if ((window == chat || window == preferences) && bot_window_open((__bridge void *)window)) {
            target = window; break;
        }
    }
    if (!target) {
        for (NSWindow *window in NSApp.orderedWindows) {
            if ((window == chat || window == preferences) && bot_window_open((__bridge void *)window)) {
                target = window; break;
            }
        }
    }
    if (!target) target = bot_window_open(history) ? chat : (bot_window_open(settings) ? preferences : nil);
    // Use the existing close hook, and never strand an attached native dialog.
    if (target && !target.attachedSheet) [target performClose:nil];
}

int bot_system_termination(void) {
    NSAppleEventDescriptor *event = NSAppleEventManager.sharedAppleEventManager.currentAppleEvent;
    NSAppleEventDescriptor *reason = [event attributeDescriptorForKeyword:kAEQuitReason] ?: [event paramDescriptorForKeyword:kAEQuitReason];
    switch (reason.enumCodeValue) {
        case kAEQuitAll: case kAEShutDown: case kAERestart: case kAEReallyLogOut: return 1;
        default: return 0;
    }
}
