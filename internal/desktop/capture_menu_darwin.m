//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import "native_darwin.h"

void bot_capture_menu_enabled(void *pointer, int enabled) {
    NSMenu *menu = (__bridge NSMenu *)pointer;
    [menu itemWithTag:2201].hidden = !enabled;
    [menu itemWithTag:2202].hidden = !enabled;
}

void bot_capture_menu_add(void *pointer, void *target, int enabled, const char *label) {
    NSMenu *menu = (__bridge NSMenu *)pointer;
    NSMenuItem *capture = [menu addItemWithTitle:[NSString stringWithUTF8String:label]
                                        action:NSSelectorFromString(@"startCapture:") keyEquivalent:@""];
    capture.target = (__bridge id)target;
    capture.tag = 2201;
    NSMenuItem *separator = NSMenuItem.separatorItem;
    separator.tag = 2202;
    [menu addItem:separator];
    bot_capture_menu_enabled(pointer, enabled);
}
