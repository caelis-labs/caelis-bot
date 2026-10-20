//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import "native_darwin.h"

static void bot_context_event(NSWindow *window) {
 NSMutableArray<NSView *> *views=[NSMutableArray arrayWithObject:window.contentView];
 while(views.count) {
  NSView *view=views.lastObject; [views removeLastObject];
  if([view isKindOfClass:WKWebView.class]) {
   [(WKWebView *)view evaluateJavaScript:@"window.dispatchEvent(new Event('context-navigation'))" completionHandler:nil]; return;
  }
  [views addObjectsFromArray:view.subviews];
 }
}

// A standard accessory stays inside AppKit's existing title bar. It owns no
// navigation authority: the active renderer applies its dialog/draft guards.
@interface BotContextNavigation : NSTitlebarAccessoryViewController
@property (weak) NSWindow *owner;
@property NSButton *button;
@end
@implementation BotContextNavigation
- (void)navigate:(id)sender {
    if (!self.owner.visible || self.owner.attachedSheet || !self.button.enabled) return;
    bot_context_event(self.owner);
}
@end
void bot_context_navigation(void *pointer, int settings, int enabled, const char *label) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    BotContextNavigation *control = nil;
    for (NSTitlebarAccessoryViewController *candidate in window.titlebarAccessoryViewControllers)
        if ([candidate isKindOfClass:BotContextNavigation.class]) { control = (BotContextNavigation *)candidate; break; }
    if (!control) {
        control = [BotContextNavigation new]; control.owner = window;
        control.layoutAttribute = NSLayoutAttributeRight;
        control.view = [[NSView alloc] initWithFrame:NSMakeRect(0,0,38,22)];
        control.button = [[NSButton alloc] initWithFrame:NSMakeRect(4,0,26,22)];
        control.button.bordered = NO; control.button.bezelStyle = NSBezelStyleAccessoryBarAction;
        control.button.target = control; control.button.action = @selector(navigate:);
        [control.view addSubview:control.button];
        [window addTitlebarAccessoryViewController:control];
    }
    NSString *text = label && *label ? [NSString stringWithUTF8String:label] : control.button.toolTip;
    control.button.image = [NSImage imageWithSystemSymbolName:settings ? @"xmark" : @"gearshape" accessibilityDescription:text];
    control.button.toolTip = text; control.button.accessibilityLabel = text;
    control.hidden = !enabled;
    control.view.hidden = !enabled;
    control.button.hidden = !enabled;
    control.button.enabled = enabled;
}

