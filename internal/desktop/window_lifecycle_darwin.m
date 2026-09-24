#import <Cocoa/Cocoa.h>
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
