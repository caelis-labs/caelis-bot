//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>
#import <CoreGraphics/CoreGraphics.h>
#import <WebKit/WebKit.h>
#import <UserNotifications/UserNotifications.h>
#import <os/log.h>
#import "native_darwin.h"
#include "panel_menu_layout.h"
#import "material_darwin.h"
#import "pet_input_darwin.h"
extern void desktopEvent(uintptr_t handle, int kind, double x, double y, double scale);
static void bot_js(NSWindow *window, NSString *js);

// Ordinary Spaces and other apps' full-screen Spaces are separate AppKit policies.
// The visible pet window owns both rendering and input, not Wails' hidden owner.
static NSWindowCollectionBehavior bot_space_behavior(BOOL pet) {
    NSWindowCollectionBehavior behavior = NSWindowCollectionBehaviorFullScreenAuxiliary;
    behavior |= pet ? NSWindowCollectionBehaviorCanJoinAllSpaces|NSWindowCollectionBehaviorStationary|NSWindowCollectionBehaviorIgnoresCycle
                    : NSWindowCollectionBehaviorMoveToActiveSpace;
    if (@available(macOS 13.0, *)) behavior |= NSWindowCollectionBehaviorCanJoinAllApplications;
    return behavior;
}

@class BotHost;
@interface BotInputPanel : NSPanel
@property BOOL interactive;
@end
@implementation BotInputPanel
- (BOOL)canBecomeKeyWindow { return self.interactive; }
- (BOOL)canBecomeMainWindow { return NO; }
@end
@interface BotHost : NSObject <NSMenuDelegate, UNUserNotificationCenterDelegate>
@property BotInputPanel *pet;
@property NSWindow *panel;
@property NSWindow *history;
@property BotInputPanel *bubble;
@property BOOL bubbleWanted;
@property BotInputPanel *prop;
@property BOOL propReady;
@property NSString *flightID;
@property NSTimer *flightTimeout;
@property NSTimer *contextTimer;
@property NSDictionary *lastContext;
@property NSDictionary *frontContext;
@property double frontSample;
@property NSUInteger contextRevision;
@property BOOL menuTracking;
@property NSMenu *trackingMenu;
@property NSData *mask;
@property uintptr_t handle;
@property id globalMonitor;
@property id localMonitor;
@property NSMutableArray *observers;
@property NSRunningApplication *previousApp;
@property BOOL dragging;
@property(readonly) BOOL pressing;
@property BotPetInputView *inputView;
@property NSTimer *dragCompletion;
@property EventHotKeyRef shortcut;
@property EventHandlerRef shortcutHandler;
@property NSString *shortcutKey;
@property int shortcutFlags;
@property UInt32 shortcutID;
@property BOOL shortcutDown;
@property BOOL panelCentered;
@property NSInteger panelContentHeight;
@property NSInteger panelMenuHeight;
@property NSScreen *panelScreen;
@property double interactionStart;
@property NSUInteger activationID;
@property NSUInteger readyID;

@property BOOL visible;
@property NSStatusItem *statusItem;
@property double scale;
@property double placementX;
@property double placementY;
@property UNAuthorizationStatus notificationPermission;
@property NSString *notificationError;
- (void)refreshNotificationPermission;
- (void)configureNotifications:(id)sender;
- (void)openSettings:(id)sender;
- (void)checkUpdates:(id)sender;
- (void)singleClick;
- (void)doubleClick;
- (void)beginDrag:(NSEvent *)event;
- (void)updateHit;
- (void)showPetWithoutActivation;
- (void)finishDrag;
- (void)anchorPanel;
- (void)focusComposer;
- (void)updateBubble;
- (void)collapseBubble;
- (void)trace:(NSString *)event;
- (void)dismissPanel:(NSString *)reason;
- (void)observeClick:(NSEvent *)event local:(BOOL)local;
- (void)openChat:(id)sender;
- (void)toggleInput:(id)sender;
- (void)togglePet:(id)sender;
- (void)quit:(id)sender;
- (NSMenu *)petMenu;
- (NSMenu *)applicationMenu;
- (void)installPreviewMenu;
- (void)dismissMenu:(NSString *)reason;
- (void)placeX:(double)x y:(double)y scale:(double)scale;
@end

@interface BotHost (Behavior)
- (NSDictionary *)desktopContext;
- (void)publishContext;
- (void)cancelPlane;
- (void)finishPlane:(NSString *)identifier outcome:(NSString *)outcome;
- (void)previewBehavior:(NSMenuItem *)item;
- (void)previewFace:(NSMenuItem *)item;
- (void)previewNear:(NSMenuItem *)item;
@end

@implementation BotHost
- (BOOL)pressing {
    return [self.inputView capturesPointer:NSEvent.mouseLocation atTime:NSProcessInfo.processInfo.systemUptime];
}
- (void)showPetWithoutActivation {
    if (!self.visible) return;
    [self.pet orderFrontRegardless];
    [self updateBubble];
}
- (void)finishDrag {
    if (!self.dragging) return;
    [self.dragCompletion invalidate]; self.dragCompletion = nil;
    self.dragging = NO;
    [self publishContext];
    NSPoint origin = self.pet.frame.origin;
    self.placementX = origin.x; self.placementY = origin.y;
    if (self.panel.visible) [self anchorPanel];
    [self updateBubble];
    [self updateHit]; [self trace:@"drag-end"];
    desktopEvent(self.handle,1,origin.x,origin.y,0);
}
- (void)openChat:(id)sender {  desktopEvent(self.handle,2,0,0,0); }
- (void)toggleInput:(id)sender { desktopEvent(self.handle,8,0,0,0); }
- (void)togglePet:(id)sender { desktopEvent(self.handle,5,!self.visible,0,0); }
- (void)quit:(id)sender { desktopEvent(self.handle,7,0,0,0); }
- (void)openSettings:(id)sender {  desktopEvent(self.handle,9,0,0,0); }
- (void)checkUpdates:(id)sender {  desktopEvent(self.handle,10,0,0,0); }
- (void)singleClick {
    if (!self.visible || !self.handle) return;
    self.interactionStart = NSProcessInfo.processInfo.systemUptime;
    [self trace:@"single-click"];
    bot_js(self.pet,@"window.dispatchEvent(new Event('pet-touch'))");
    desktopEvent(self.handle,8,0,0,0);
}
- (void)doubleClick {
    if (!self.visible || !self.handle) return;
    self.interactionStart = NSProcessInfo.processInfo.systemUptime;
    [self trace:@"double-click"];
    bot_js(self.pet,@"window.dispatchEvent(new Event('pet-touch'))");
    desktopEvent(self.handle,12,0,0,0);
}
- (void)beginDrag:(NSEvent *)event {
    if (!self.visible || !self.handle) return;
    self.dragging = YES;
    self.pet.ignoresMouseEvents = NO;
    self.interactionStart = NSProcessInfo.processInfo.systemUptime;
    [self cancelPlane]; [self publishContext];
    [self.bubble orderOut:nil];
    [self dismissPanel:@"panel-dismiss-drag"];
    [self trace:@"drag-start"];
    [self.pet performWindowDragWithEvent:event];
    // Window Server owns movement and may consume mouseUp. Observe release
    // only for an active drag; never run a mouse tracking loop for clicks.
    if (!self.dragging) return;
    if (!(NSEvent.pressedMouseButtons & 1)) { [self finishDrag]; return; }
    __weak BotHost *weak = self;
    self.dragCompletion = [NSTimer timerWithTimeInterval:1.0/60 repeats:YES block:^(NSTimer *timer) {
        if (!(NSEvent.pressedMouseButtons & 1)) [weak finishDrag];
    }];
    [NSRunLoop.mainRunLoop addTimer:self.dragCompletion forMode:NSRunLoopCommonModes];
}
- (void)refreshNotificationPermission {
    __weak BotHost *weak = self;
    [UNUserNotificationCenter.currentNotificationCenter getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
        dispatch_async(dispatch_get_main_queue(), ^{ weak.notificationPermission = settings.authorizationStatus; });
    }];
}
- (void)configureNotifications:(id)sender {
    if (self.notificationPermission == UNAuthorizationStatusNotDetermined) {
        __weak BotHost *weak = self;
        [UNUserNotificationCenter.currentNotificationCenter requestAuthorizationWithOptions:UNAuthorizationOptionAlert | UNAuthorizationOptionSound completionHandler:^(BOOL granted, NSError *error) {
            dispatch_async(dispatch_get_main_queue(), ^{
                if (!weak) return;
                weak.notificationError = error ? @"暂时无法开启通知，请稍后重试" : nil;
                [weak refreshNotificationPermission];
                if (granted) bot_notify((__bridge void *)weak, "notifications-enabled", "通知已开启", "到期提醒会出现在这里，点击通知可打开 Caelis Bot。", 1);
            });
        }];
    } else {
        [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:@"x-apple.systempreferences:com.apple.Notifications-Settings.extension"]];
    }
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
    completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList | UNNotificationPresentationOptionSound);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))completionHandler {
    __weak BotHost *weak = self;
    dispatch_async(dispatch_get_main_queue(), ^{ if (weak.handle) desktopEvent(weak.handle,2,0,0,0); });
    completionHandler();
}
- (NSMenu *)petMenu {
    NSMenu *menu = [NSMenu new];
    menu.autoenablesItems = NO; menu.delegate = self;
    NSMenuItem *open = [menu addItemWithTitle:@"对话" action:@selector(openChat:) keyEquivalent:@""];
    open.target = self;
    NSMenuItem *visibility = [menu addItemWithTitle:self.visible ? @"隐藏" : @"显示" action:@selector(togglePet:) keyEquivalent:@""];
    visibility.target = self; visibility.tag = 2;
    return menu;
}
- (NSMenu *)applicationMenu {
    NSMenu *menu = [self petMenu];
    [menu addItem:NSMenuItem.separatorItem];
    NSMenuItem *settings = [menu addItemWithTitle:@"设置…" action:@selector(openSettings:) keyEquivalent:@","];
    settings.target = self;
    NSMenuItem *updates = [menu addItemWithTitle:@"检查更新…" action:@selector(checkUpdates:) keyEquivalent:@""];
    updates.target = self;
    [menu addItem:NSMenuItem.separatorItem];
    NSMenuItem *quit = [menu addItemWithTitle:@"退出" action:@selector(quit:) keyEquivalent:@"q"];
    quit.target = self;
    return menu;
}
- (void)installPreviewMenu {
    // Explicit developer opt-in belongs in the application menu bar, never on the pet.
    if ([NSProcessInfo.processInfo.environment[@"CAELIS_BOT_BEHAVIOR_PREVIEW"] isEqualToString:@"1"]) {
        NSMenuItem *preview=[NSApp.mainMenu addItemWithTitle:@"开发预览动作" action:nil keyEquivalent:@""];
        NSMenu *sub=[NSMenu new];NSArray *names=@[@"轻呼吸",@"观察",@"换重心",@"整理纸飞机",@"舒展",@"纸飞机绕行"];
        for(NSInteger i=0;i<names.count;i++){NSMenuItem *item=[sub addItemWithTitle:names[i] action:@selector(previewBehavior:) keyEquivalent:@""];item.target=self;item.tag=i;}
        [sub addItem:NSMenuItem.separatorItem];
        NSArray *expressions=@[@"表情 · 眨眼",@"表情 · 闭眼笑",@"表情 · 好奇",@"表情 · 微笑",@"表情 · 专注",@"表情 · 期待"];
        for(NSInteger i=0;i<expressions.count;i++){NSMenuItem *item=[sub addItemWithTitle:expressions[i] action:@selector(previewFace:) keyEquivalent:@""];item.target=self;item.tag=i;}
        NSArray *gestures=@[@"近身 · 挥手",@"近身 · 思考",@"近身 · 询问"];
        for(NSInteger i=0;i<gestures.count;i++){NSMenuItem *item=[sub addItemWithTitle:gestures[i] action:@selector(previewNear:) keyEquivalent:@""];item.target=self;item.tag=i;}
        preview.submenu=sub;
    }
}
- (void)dismissMenu:(NSString *)reason {
    NSMenu *menu = self.trackingMenu;
    if (!menu) return;
    self.trackingMenu = nil; self.menuTracking = NO;
    [menu cancelTrackingWithoutAnimation];
    [self publishContext]; [self trace:reason];
}
- (void)menuDidClose:(NSMenu *)menu {
    if (self.trackingMenu != menu) return;
    self.trackingMenu=nil; self.menuTracking=NO; [self publishContext];
}
- (void)menuWillOpen:(NSMenu *)menu {
    self.trackingMenu=menu; self.menuTracking=YES; [self cancelPlane]; [self publishContext];

    for (NSMenuItem *item in menu.itemArray) {
        if (item.tag == 2) item.title = self.visible ? @"隐藏" : @"显示";
    }
}
- (void)placeX:(double)x y:(double)y scale:(double)scale {
    [self cancelPlane];
    self.placementX = x; self.placementY = y; self.scale = scale;
    NSRect frame = NSMakeRect(x,y,180*scale,240*scale);
    [self.pet setFrame:frame display:YES];
    [self updateBubble];
    [self updateHit];
}
- (void)dismissPanel:(NSString *)reason {
    if (!self.panel.visible || self.panel.attachedSheet) return;
    // Outside clicks keep their destination and its focus. Never activate the
    // previous app here: the user may have chosen a different app altogether.
    [self.panel orderOut:nil];
    self.panel.level = NSFloatingWindowLevel;
    self.panelMenuHeight = 0;
    self.previousApp = nil;
    bot_js(self.panel,@"window.dispatchEvent(new Event('panel-close'))");
    [self updateBubble];
    [self trace:reason];
}
- (void)collapseBubble {
 if (!self.bubble.interactive) return;
 self.bubble.interactive=NO; [self.bubble resignKeyWindow];
 bot_js(self.bubble,@"window.dispatchEvent(new Event('bubble-collapse'))");
}
- (void)observeClick:(NSEvent *)event local:(BOOL)local {
    if (self.dragging && event.type == NSEventTypeLeftMouseUp) [self finishDrag];
    [self updateHit];
    if (event.type != NSEventTypeLeftMouseDown && event.type != NSEventTypeRightMouseDown && event.type != NSEventTypeOtherMouseDown) return;
    if (!local || event.window != self.pet || event.type != NSEventTypeLeftMouseDown) [self.inputView cancelInteraction];
    // Accessory apps may already be inactive when their status menu opens, so
    // another app's click need not produce NSApplicationDidResignActive again.
    // Cancel only proven outside clicks; keep menu-item/windowless local events
    // with AppKit and return the original event to preserve its destination.
    if (!local || (event.window && (event.window==self.pet || event.window==self.panel || event.window==self.history || event.window==self.bubble || event.window==self.prop))) {
        [self dismissMenu:local ? @"menu-dismiss-local" : @"menu-dismiss-global"];
    }
    if (!local || (event.window && event.window != self.bubble)) [self collapseBubble];
    // Windowless local events (for example native menus) are not evidence of
    // an outside click. App deactivation independently covers other apps.
    if (!local || (event.window && event.window != self.panel && event.window != self.pet)) [self dismissPanel:local ? @"panel-dismiss-local" : @"panel-dismiss-global"];
}
- (void)trace:(NSString *)event {
    static os_log_t logger;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ logger = os_log_create("dev.caelis.bot", "Desktop"); });
    if (![event hasPrefix:@"hit-"]) os_log_info(logger, "Surface event: %{public}@", event);
    NSString *path = NSProcessInfo.processInfo.environment[@"CAELIS_BOT_DESKTOP_TRACE"];
    if (!path.length) return;
    NSRect r = self.pet.frame;
    NSMutableArray *screens = [NSMutableArray new];
    for (NSScreen *s in NSScreen.screens) [screens addObject:@{@"frame":NSStringFromRect(s.frame),@"visibleFrame":NSStringFromRect(s.visibleFrame),@"scale":@(s.backingScaleFactor)}];
    NSDictionary *record = @{@"event":event,@"time":@(NSDate.date.timeIntervalSince1970),@"x":@(r.origin.x),@"y":@(r.origin.y),@"width":@(r.size.width),@"height":@(r.size.height),@"visible":@(self.pet.visible),@"petOnActiveSpace":@(self.pet.onActiveSpace),@"petOcclusionVisible":@((self.pet.occlusionState & NSWindowOcclusionStateVisible)!=0),@"inputOnActiveSpace":@(self.pet.onActiveSpace),@"singlePetSurface":@YES,@"panelVisible":@(self.panel.visible),@"panelOnActiveSpace":@(self.panel.onActiveSpace),@"panelFrame":NSStringFromRect(self.panel.frame),@"panelContentHeight":@(self.panelContentHeight),@"panelMenuHeight":@(self.panelMenuHeight),@"panelLevel":@(self.panel.level),@"petLevel":@(self.pet.level),@"panelKey":@(self.panel.keyWindow),@"panelResponder":NSStringFromClass(self.panel.firstResponder.class)?:@"none",@"doubleClickInterval":@(NSEvent.doubleClickInterval),@"petKey":@(self.pet.keyWindow),@"bubbleVisible":@(self.bubble.visible),@"bubbleKey":@(self.bubble.keyWindow),@"bubbleFrame":NSStringFromRect(self.bubble.frame),@"frontApp":NSWorkspace.sharedWorkspace.frontmostApplication.bundleIdentifier ?: @"",@"activationPolicy":@(NSApp.activationPolicy),@"propWindowNumber":@(self.prop.windowNumber),@"propVisible":@(self.prop.visible),@"propKey":@(self.prop.keyWindow),@"propClickThrough":@(self.prop.ignoresMouseEvents),@"propFrame":NSStringFromRect(self.prop.frame),@"maskBytes":@(self.mask.length),@"screens":screens};
    NSData *data = [NSJSONSerialization dataWithJSONObject:record options:NSJSONWritingSortedKeys error:nil];
    if (![NSFileManager.defaultManager fileExistsAtPath:path]) [NSFileManager.defaultManager createFileAtPath:path contents:nil attributes:@{NSFilePosixPermissions:@0600}];
    NSFileHandle *file = [NSFileHandle fileHandleForWritingAtPath:path];
    [file seekToEndOfFile]; [file writeData:data]; [file writeData:[@"\n" dataUsingEncoding:NSUTF8StringEncoding]]; [file closeFile];
}
- (void)updateHit {
    NSPoint point = NSEvent.mouseLocation;
    if (self.dragging || [self.inputView capturesPointer:point atTime:NSProcessInfo.processInfo.systemUptime]) return;
    NSRect frame = self.pet.frame;
    BOOL hit = NO;
    if (self.visible && self.mask.length == 180*240 && NSPointInRect(point,frame)) {
        int x = (int)((point.x-frame.origin.x)*180/frame.size.width);
        int y = (int)((point.y-frame.origin.y)*240/frame.size.height);
        const unsigned char *bytes = self.mask.bytes;
        hit = x>=0 && x<180 && y>=0 && y<240 && bytes[y*180+x] != 0;
    }
    if (self.pet.ignoresMouseEvents == hit) {
        self.pet.ignoresMouseEvents = !hit;
        [self trace:hit ? @"hit-enter" : @"hit-leave"];
    }
}
// Activating an accessory app and focusing its WebKit editor are separate steps.
// A global hotkey has no mouse click to install WebKit as first responder.
- (void)focusComposer {
    if (!self.panel.visible || !self.panel.keyWindow || self.panel.attachedSheet || !NSApp.active) return;
    NSMutableArray *views=[NSMutableArray arrayWithObject:self.panel.contentView];
    while(views.count){
        NSView *view=views.lastObject;[views removeLastObject];
        if([view isKindOfClass:WKWebView.class]){
            NSResponder *current=self.panel.firstResponder;
            if(![current isKindOfClass:NSView.class] || ![(NSView *)current isDescendantOf:view]) [self.panel makeFirstResponder:view];
            bot_js(self.panel,@"window.dispatchEvent(new Event('panel-focus'))");
            [self trace:@"panel-focus"];
            return;
        }
        [views addObjectsFromArray:view.subviews];
    }
}
- (void)anchorPanel {
    NSRect pet = self.pet.frame, panel = self.panel.frame;
    NSScreen *screen = self.panelCentered ? self.panelScreen : self.pet.screen;
    if (![NSScreen.screens containsObject:screen]) screen = NSScreen.mainScreen;
    NSRect bounds = screen.visibleFrame;
    panel.size.width = MIN(self.panelCentered ? 620 : 420, bounds.size.width-32);
    panel.size.height = self.panelContentHeight;
    if (self.panelCentered) {
        panel.origin = NSMakePoint(NSMidX(bounds)-panel.size.width/2, NSMidY(bounds)-panel.size.height/2);
    } else {
        // Anchor the input, not the combined input + popup envelope.
        double x = NSMidX(pet)-panel.size.width/2;
        x = MAX(NSMinX(bounds),MIN(x,NSMaxX(bounds)-panel.size.width));
        double y = NSMinY(pet)+44*(pet.size.height/240)-panel.size.height-10;
        if (y < NSMinY(bounds)+8) y = NSMaxY(pet)+10;
        y = MAX(NSMinY(bounds)+8,MIN(y,NSMaxY(bounds)-panel.size.height-8));
        panel.origin = NSMakePoint(x,y);
    }
    BotPanelMenuLayout layout = bot_panel_menu_layout(panel.origin.y,panel.size.width,panel.size.height,
        NSMinY(bounds),NSMaxY(bounds),self.panelMenuHeight);
    panel.origin.y = layout.originY;
    panel.size.height = layout.height;
    self.panel.hasShadow = self.panelMenuHeight == 0;
    // The pet is also floating; a menu must remain above its independent window.
    self.panel.level = NSFloatingWindowLevel + (self.panelMenuHeight > 0 ? 1 : 0);
    [self.panel setFrame:panel display:YES];
    bot_layout_input_material(self.panel,layout.inputTop,self.panelContentHeight);
    bot_js(self.panel,[NSString stringWithFormat:@"document.documentElement.style.setProperty('--composer-top','%gpx');window.dispatchEvent(new CustomEvent('panel-menu-layout',{detail:{activation:%lu,left:%g,top:%g,width:%g,height:%g}}))",layout.inputTop,(unsigned long)self.activationID,layout.menuLeft,layout.menuTop,layout.menuWidth,layout.menuHeight]);
}

- (void)updateBubble {
    if (!self.visible || !self.bubbleWanted || self.dragging || self.panel.visible || self.history.keyWindow) {
        [self.bubble orderOut:nil]; return;
    }
    NSRect pet = self.pet.frame, bubble = self.bubble.frame;
    NSRect bounds = (self.pet.screen ?: NSScreen.mainScreen).visibleFrame;
    double x = MAX(NSMinX(bounds)+8,MIN(NSMidX(pet)-bubble.size.width/2,NSMaxX(bounds)-bubble.size.width-8));
    double y = NSMaxY(pet)+8;
    // At the top edge, use a side position before covering the character.
    if (y+bubble.size.height > NSMaxY(bounds)-8) {
        x = NSMinX(pet)-bubble.size.width-8;
        if (x < NSMinX(bounds)+8) x = NSMaxX(pet)+8;
        x = MAX(NSMinX(bounds)+8,MIN(x,NSMaxX(bounds)-bubble.size.width-8));
        y = NSMaxY(pet)-bubble.size.height;
    }
    y = MAX(NSMinY(bounds)+8,MIN(y,NSMaxY(bounds)-bubble.size.height-8));
    [self.bubble setFrameOrigin:NSMakePoint(x,y)];
    if (!self.bubble.visible) [self.bubble orderFrontRegardless];
}
@end

static void bot_js(NSWindow *window, NSString *js) {
    // Wails owns the WKWebView. Find it without importing Wails private headers.
    NSMutableArray *views = [NSMutableArray arrayWithObject:window.contentView];
    while (views.count) {
        NSView *view = views.lastObject; [views removeLastObject];
        if ([view isKindOfClass:WKWebView.class]) {
            [(WKWebView *)view evaluateJavaScript:js completionHandler:nil]; return;
        }
        [views addObjectsFromArray:view.subviews];
    }
}
#include "behavior_darwin.h"

void *bot_create(void *pet, void *panel, void *bubble, void *history, void *prop, uintptr_t handle, unsigned char *icon, int iconLength) {
    BotHost *host = [BotHost new];
    UNUserNotificationCenter.currentNotificationCenter.delegate = host;
    [host refreshNotificationPermission];
    NSWindow *renderOwner = (__bridge NSWindow *)pet;
    host.history = (__bridge NSWindow *)history;
    host.history.backgroundColor = NSColor.windowBackgroundColor;
    host.panel = (__bridge NSWindow *)panel; host.handle = handle;
    host.panel.collectionBehavior = bot_space_behavior(NO);
    host.panel.animationBehavior = NSWindowAnimationBehaviorNone;
    host.panelContentHeight = 64;
    bot_install_material(host.panel, 31, 0, 0);
    host.scale = 1;
    host.statusItem = [NSStatusBar.systemStatusBar statusItemWithLength:NSSquareStatusItemLength];
    NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:icon length:iconLength]];
    // The full-color character has transparent breathing room of its own.
    // Fill more of the status item while retaining a margin on shorter bars.
    CGFloat iconSize = MIN(24.0, NSStatusBar.systemStatusBar.thickness - 2.0);
    image.size = NSMakeSize(iconSize,iconSize);
    host.statusItem.button.image = image;
    host.statusItem.button.toolTip = @"Caelis Bot";
    host.statusItem.button.accessibilityLabel = @"Caelis Bot";
    host.statusItem.menu = [host applicationMenu];
    [host installPreviewMenu];
    // Wails beta.6 cannot create NSPanel. Keep its original hidden window and
    // protocol/webview ownership, and mount its content in a non-key NSPanel.
    // Do not change the runtime class of an initialized AppKit window: dynamic
    // NSWindow properties on macOS 27 do not support that operation.
    host.pet = [[BotInputPanel alloc] initWithContentRect:renderOwner.frame styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    NSView *content = renderOwner.contentView;
    renderOwner.contentView = [[NSView alloc] initWithFrame:content.bounds];
    // One nonactivating window owns WebKit AND its native hit view. Separate
    // parent/child windows drifted after Space/full-screen ordering: only the
    // input window moved until the final Go placement write caught the pet up.
    NSView *surface = [[NSView alloc] initWithFrame:content.bounds];
    content.autoresizingMask = NSViewWidthSizable|NSViewHeightSizable;
    [surface addSubview:content];
    host.pet.contentView = surface;
    host.pet.title = @"Caelis Bot — 桌宠";
    host.pet.opaque = NO; host.pet.backgroundColor = NSColor.clearColor;
    host.pet.hasShadow = NO; host.pet.level = NSFloatingWindowLevel;
    host.pet.hidesOnDeactivate = NO;
    host.pet.releasedWhenClosed = NO;
    host.pet.collectionBehavior = bot_space_behavior(YES);
    host.pet.ignoresMouseEvents = YES;
    host.pet.movableByWindowBackground = NO;
    // The message surface is non-key too. It never joins the pet's drag loop.
    NSWindow *bubbleOwner = (__bridge NSWindow *)bubble;
    // The 56 pt capsule has a 6 pt transparent gutter for its soft shadow.
    NSRect bubbleFrame = bubbleOwner.frame; bubbleFrame.size = NSMakeSize(360,68);
    host.bubble = [[BotInputPanel alloc] initWithContentRect:bubbleFrame styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    NSView *bubbleContent = bubbleOwner.contentView;
    bubbleOwner.contentView = [[NSView alloc] initWithFrame:bubbleContent.bounds];
    NSView *bubbleSurface = [[NSView alloc] initWithFrame:NSMakeRect(0,0,360,68)];
    bubbleContent.frame = bubbleSurface.bounds;
    bubbleContent.autoresizingMask = NSViewWidthSizable|NSViewHeightSizable;
    [bubbleSurface addSubview:bubbleContent];
    host.bubble.contentView = bubbleSurface;
    bot_install_material(host.bubble, 28, 0, 6);
    host.bubble.title = @"Caelis Bot — 消息";
    host.bubble.opaque = NO; host.bubble.backgroundColor = NSColor.clearColor;
    host.bubble.hasShadow = NO; host.bubble.level = NSFloatingWindowLevel;
    host.bubble.hidesOnDeactivate = NO; host.bubble.releasedWhenClosed = NO;
    host.bubble.collectionBehavior = bot_space_behavior(YES);
    NSWindow *propOwner=(__bridge NSWindow *)prop;
    host.prop=[[BotInputPanel alloc] initWithContentRect:propOwner.frame styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    NSView *propContent=propOwner.contentView;
    propOwner.contentView=[[NSView alloc] initWithFrame:propContent.bounds];
    host.prop.contentView=propContent;
    host.prop.title=@"Caelis Bot — 纸飞机";
    host.prop.opaque=NO;host.prop.backgroundColor=NSColor.clearColor;host.prop.hasShadow=NO;
    host.prop.level=NSFloatingWindowLevel;host.prop.hidesOnDeactivate=NO;host.prop.releasedWhenClosed=NO;
    host.prop.collectionBehavior=bot_space_behavior(YES);host.prop.ignoresMouseEvents=YES;
    __weak BotHost *weak = host;
    BotPetInputView *view = [[BotPetInputView alloc] initWithFrame:surface.bounds];
    host.inputView = view;
    view.autoresizingMask = NSViewWidthSizable|NSViewHeightSizable;
    view.singleClick = ^{ [weak singleClick]; };
    view.doubleClick = ^{ [weak doubleClick]; };
    view.beginDrag = ^(NSEvent *event){ [weak beginDrag:event]; };
    view.pointerChanged = ^{ [weak updateHit]; };
    view.contextMenu = ^NSMenu *{ return [weak petMenu]; };
    [surface addSubview:view positioned:NSWindowAbove relativeTo:content];
    view.accessibilityElement = YES;
    view.accessibilityRole = NSAccessibilityButtonRole;
    view.accessibilityLabel = @"Caelis Bot 桌宠";
    view.accessibilityHelp = @"单击展开或收起输入框，双击唤回聊天和已打开的设置；右键菜单，可拖动";
    NSEventMask mask = NSEventMaskMouseMoved|NSEventMaskLeftMouseDragged|NSEventMaskLeftMouseDown|NSEventMaskLeftMouseUp|NSEventMaskRightMouseDown|NSEventMaskOtherMouseDown;
    host.globalMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:mask handler:^(NSEvent *event) { [weak observeClick:event local:NO]; }];
    host.localMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *event) { [weak observeClick:event local:YES]; return event; }];
    host.contextTimer=[NSTimer timerWithTimeInterval:1.0/8 repeats:YES block:^(NSTimer *timer){[weak publishContext];}];
    [NSRunLoop.mainRunLoop addTimer:host.contextTimer forMode:NSRunLoopCommonModes];
    host.observers = [NSMutableArray new];
    id moved = [NSNotificationCenter.defaultCenter addObserverForName:NSWindowDidMoveNotification object:host.pet queue:nil usingBlock:^(NSNotification *note) {
        if (weak.dragging && !(NSEvent.pressedMouseButtons & 1)) [weak finishDrag];
    }];
    [host.observers addObject:moved];
    id display = [NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidChangeScreenParametersNotification object:nil queue:nil usingBlock:^(NSNotification *note) {
        if (weak) { [weak cancelPlane]; weak.frontContext=nil; desktopEvent(weak.handle,3,0,0,0); }
    }];
    [host.observers addObject:display];
    id deactivate = [NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidResignActiveNotification object:nil queue:nil usingBlock:^(NSNotification *note) { [weak.inputView cancelInteraction]; [weak dismissMenu:@"menu-dismiss-deactivate"];  [weak dismissPanel:@"panel-dismiss-deactivate"]; [weak collapseBubble]; [weak updateBubble]; }];
    [host.observers addObject:deactivate];
    for(NSString *name in @[NSApplicationDidBecomeActiveNotification,NSWindowDidBecomeKeyNotification]){
        id target=[name isEqualToString:NSWindowDidBecomeKeyNotification]?host.panel:nil;
        id observer=[NSNotificationCenter.defaultCenter addObserverForName:name object:target queue:nil usingBlock:^(NSNotification *note){
            // Finish native activation first, then focus only the still-open panel.
            dispatch_async(dispatch_get_main_queue(),^{[weak focusComposer];});
        }];
        [host.observers addObject:observer];
    }
    id activated = [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:NSWorkspaceDidActivateApplicationNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
        NSRunningApplication *app = note.userInfo[NSWorkspaceApplicationKey];
        if (app && app.processIdentifier != NSProcessInfo.processInfo.processIdentifier) [weak dismissMenu:@"menu-dismiss-other-app"];
    }];
    [host.observers addObject:activated];
    for (NSString *name in @[NSWindowDidBecomeKeyNotification,NSWindowDidResignKeyNotification,NSWindowDidMiniaturizeNotification,NSWindowDidDeminiaturizeNotification]) {
        id observer=[NSNotificationCenter.defaultCenter addObserverForName:name object:host.history queue:nil usingBlock:^(NSNotification *note) { [weak updateBubble]; }];
        [host.observers addObject:observer];
    }
    // AppKit's public window state distinguishes ordered-in from actually being
    // on the active Space. This is diagnostic evidence, not a visibility policy.
    for (NSWindow *window in @[host.pet,host.panel,host.bubble]) {
        id observer = [NSNotificationCenter.defaultCenter addObserverForName:NSWindowDidChangeOcclusionStateNotification object:window queue:nil usingBlock:^(NSNotification *note) {
            [weak trace:@"occlusion-change"];
        }];
        [host.observers addObject:observer];
    }
    for (NSString *name in @[NSWorkspaceDidWakeNotification,NSWorkspaceActiveSpaceDidChangeNotification]) {
        id observer = [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:name object:nil queue:nil usingBlock:^(NSNotification *note) {
            if (weak) {
                [weak.inputView cancelInteraction]; [weak dismissMenu:@"menu-dismiss-space-or-wake"]; [weak cancelPlane]; weak.frontContext=nil;
                if ([note.name isEqualToString:NSWorkspaceDidWakeNotification]) {
                    [weak trace:@"wake"];
                    desktopEvent(weak.handle,3,0,0,0);
                } else {
                    // Switching desktops is not a geometry change or an open-input
                    // request. Preserve placement/hidden preference and user focus.
                     [weak dismissPanel:@"panel-dismiss-space-change"]; [weak collapseBubble];
                    [weak showPetWithoutActivation];
                    [weak updateHit];
                    [weak trace:@"active-space-change"];
                }
            }
        }];
        [host.observers addObject:observer];
    }
    return (__bridge_retained void *)host;
}
int bot_screens(BotRect *rects, int capacity) {
    NSArray<NSScreen *> *screens = NSScreen.screens;
    int count = MIN((int)screens.count,capacity);
    for (int i=0;i<count;i++) { NSRect r = screens[i].visibleFrame; rects[i]=(BotRect){r.origin.x,r.origin.y,r.size.width,r.size.height}; }
    return count;
}
void bot_apply(void *pointer, double x, double y, double scale, int visible) {
    BotHost *host = (__bridge BotHost *)pointer;
    if (!visible) [host.inputView cancelInteraction];
    host.visible = visible;
    host.contextTimer.fireDate=visible ? NSDate.date : NSDate.distantFuture;
    if(!visible)host.lastContext=nil;
    [host placeX:x y:y scale:scale];
    if (visible) {
        [NSApp unhideWithoutActivation];
        [host showPetWithoutActivation];
        if (host.panel.visible) [host.panel orderFront:nil];
    } else {  [host.pet orderOut:nil]; [host.bubble orderOut:nil]; }
    if (host.panel.visible) [host anchorPanel];
    [host updateHit];
    [host trace:@"apply"];
    bot_js(host.pet,visible ? @"window.dispatchEvent(new CustomEvent('pet-visibility',{detail:true}))" : @"window.dispatchEvent(new CustomEvent('pet-visibility',{detail:false}))");
    bot_js(host.bubble,visible ? @"window.dispatchEvent(new CustomEvent('pet-visibility',{detail:true}))" : @"window.dispatchEvent(new CustomEvent('pet-visibility',{detail:false}))");
}
void bot_bubble(void *pointer, int visible) {
    BotHost *host = (__bridge BotHost *)pointer;
    host.bubbleWanted = visible;
    [host updateBubble];
}
void bot_panel(void *pointer, int visible) {
    BotHost *host = (__bridge BotHost *)pointer;
    if (visible) {
        [host cancelPlane];
        [host collapseBubble];
        NSRunningApplication *front = NSWorkspace.sharedWorkspace.frontmostApplication;
        if (front.processIdentifier != NSProcessInfo.processInfo.processIdentifier) host.previousApp = front;
        host.panelMenuHeight = 0;
        host.activationID++;
        [host anchorPanel];
        [NSApp activateIgnoringOtherApps:YES]; [host.panel makeKeyAndOrderFront:nil];
        [host focusComposer];
        bot_js(host.panel,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('panel-open',{detail:{activation:%lu}}))",(unsigned long)host.activationID]);
    } else {
        BOOL restore = NSApp.active && host.panel.keyWindow;
        [host.panel orderOut:nil];
        host.panel.level = NSFloatingWindowLevel;
        host.panelMenuHeight = 0;
        if (restore && host.previousApp && !host.previousApp.terminated) [host.previousApp activateWithOptions:0];
        host.previousApp = nil;
        bot_js(host.panel,@"window.dispatchEvent(new Event('panel-close'))");
    }
    [host updateBubble];
    [host trace:visible ? @"panel-open" : @"panel-close"];
}
int bot_prepare_window_recall(void *pointer) {
    BotHost *host = (__bridge BotHost *)pointer;
    [host.inputView cancelInteraction];
    if (host.panel.attachedSheet) {
        [NSApp activateIgnoringOtherApps:YES];
        [host.panel makeKeyAndOrderFront:nil];
        [host.panel.attachedSheet makeKeyAndOrderFront:nil];
        return 0;
    }
    // This transition stays in Bot. Explicitly dismiss without restoring the
    // old foreground app, which can asynchronously steal the new window focus.
    [host dismissPanel:@"panel-dismiss-window-recall"];
    host.previousApp = nil;
    [host collapseBubble];
    return 1;
}
int bot_window_open(void *pointer) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    return window.visible || window.miniaturized;
}
int bot_window_visible(void *pointer) {
    return [(__bridge NSWindow *)pointer isVisible];
}
void bot_toggle_panel(void *pointer) {
    BotHost *host = (__bridge BotHost *)pointer;
    if (host.panel.attachedSheet) return;
    BOOL close = host.panel.visible && !host.panelCentered;
    host.panelCentered = NO;
    bot_panel(pointer,!close);
}
void bot_panel_height(void *pointer, int height) {
    BotHost *host = (__bridge BotHost *)pointer;
    host.panelContentHeight = height;
    [host anchorPanel];
    [host trace:@"panel-layout"];
}
void bot_panel_menu(void *pointer, int height, int activation) {
    BotHost *host = (__bridge BotHost *)pointer;
    if (!host.panel.visible || host.activationID != (NSUInteger)activation) return;
    host.panelMenuHeight = height;
    [host anchorPanel];
    [host trace:height ? @"panel-menu-open" : @"panel-menu-close"];
}
void bot_mask(void *pointer, unsigned char *mask, int length) {
    BotHost *host = (__bridge BotHost *)pointer;
    host.mask = [NSData dataWithBytes:mask length:length]; [host updateHit]; [host trace:@"hit-mask"];
}
void bot_destroy(void *pointer) {
    BotHost *host = (__bridge_transfer BotHost *)pointer;
    [host trace:@"shutdown"];
    UNUserNotificationCenter.currentNotificationCenter.delegate = nil;
    [host dismissMenu:@"menu-dismiss-shutdown"];
    [host cancelPlane]; [host.contextTimer invalidate];
    [host.inputView cancelInteraction];
    host.handle = 0;
    if(host.shortcut) UnregisterEventHotKey(host.shortcut);
    if(host.shortcutHandler) RemoveEventHandler(host.shortcutHandler);
    if (host.globalMonitor) [NSEvent removeMonitor:host.globalMonitor];
    if (host.localMonitor) [NSEvent removeMonitor:host.localMonitor];
    for (id observer in host.observers) {
        [NSNotificationCenter.defaultCenter removeObserver:observer];
        [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:observer];
    }
    [NSStatusBar.systemStatusBar removeStatusItem:host.statusItem];
    [host.dragCompletion invalidate];

    [host.pet close]; [host.prop close]; [host.bubble close]; [host.panel orderOut:nil];
}

void bot_expand_bubble(void *pointer,int expanded) {
 BotHost *host=(__bridge BotHost *)pointer;
 if(!expanded){[host collapseBubble];return;}
 [host dismissPanel:@"panel-dismiss-approval"];
 host.bubble.interactive=YES; host.bubbleWanted=YES;
 [host updateBubble];
 // Only this explicit action enables keyboard entry. Arrival stays non-key.
 [host.bubble makeKeyAndOrderFront:nil];
 bot_js(host.bubble,@"window.dispatchEvent(new Event('bubble-expand'))");
}
void bot_bubble_height(void *pointer,int height) {
 BotHost *host=(__bridge BotHost *)pointer;
 NSRect frame=host.bubble.frame; frame.size=NSMakeSize(360,height);
 [host.bubble setFrame:frame display:YES];[host updateBubble];
}

void bot_gesture(void *pointer,char *action) {
 BotHost *host=(__bridge BotHost *)pointer;if(!host.visible)return;
 NSString *name=[NSString stringWithUTF8String:action];
 if(![@[@"attention",@"nod",@"celebrate"] containsObject:name])return;
 [host trace:[@"gesture-" stringByAppendingString:name]];
 NSData *data=[NSJSONSerialization dataWithJSONObject:@[name] options:0 error:nil];
 NSString *json=[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
 bot_js(host.pet,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('pet-gesture',{detail:(%@)[0]}))",json]);
}

void bot_notify(void *pointer, char *identifier, char *title, char *body, int reminder) {
    BotHost *host = (__bridge BotHost *)pointer;
    // Ordinary results already have a pet bubble. A hidden pet must not silence
    // due reminders or decisions. Arrival never activates a window.
    if (host.history.keyWindow || host.panel.keyWindow || (host.visible && !reminder)) return;
    UNMutableNotificationContent *content = [UNMutableNotificationContent new];
    content.title = [NSString stringWithUTF8String:title];
    content.body = [NSString stringWithUTF8String:body];
    content.sound = UNNotificationSound.defaultSound;
    UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:[NSString stringWithUTF8String:identifier] content:content trigger:nil];
    __weak BotHost *weak = host;
    [UNUserNotificationCenter.currentNotificationCenter addNotificationRequest:request withCompletionHandler:^(NSError *error) {
        dispatch_async(dispatch_get_main_queue(), ^{ weak.notificationError = error ? @"系统通知未能送达，请检查通知权限" : nil; });
    }];
}

int bot_notification_status(void *pointer) {
    BotHost *host = (__bridge BotHost *)pointer;
    [host refreshNotificationPermission];
    return host.notificationError ? -1 : (int)host.notificationPermission;
}
void bot_configure_notifications(void *pointer) {
    [(__bridge BotHost *)pointer configureNotifications:nil];
}
int bot_trash_path(char *path) {
    NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
    return [NSFileManager.defaultManager trashItemAtURL:url resultingItemURL:nil error:nil] ? 1 : 0;
}

void bot_style_settings(void *pointer) {
    NSWindow *window = (__bridge NSWindow *)pointer;
    window.backgroundColor = NSColor.windowBackgroundColor;
    bot_install_material(window, 16, 184, 0);
}

void bot_activity(void *pointer, char *activity) {
    BotHost *host = (__bridge BotHost *)pointer;
    NSString *state = [NSString stringWithUTF8String:activity];
    if (![@[@"idle",@"working",@"waiting"] containsObject:state]) return;
    if (![state isEqualToString:@"idle"]) [host cancelPlane];
    bot_js(host.pet,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('pet-activity',{detail:'%@'}))",state]);
}

// Carbon hotkeys are system registrations; no keyboard surveillance permission.
static OSStatus bot_hotkey(EventHandlerCallRef next, EventRef event, void *context) {
    BotHost *host=(__bridge BotHost *)context;
    EventHotKeyID key;
    if(GetEventParameter(event,kEventParamDirectObject,typeEventHotKeyID,NULL,sizeof(key),NULL,&key)!=noErr || key.signature!='Cael' || key.id!=host.shortcutID) return eventNotHandledErr;
    if(GetEventKind(event)==kEventHotKeyReleased){host.shortcutDown=NO;return noErr;}
    if(host.shortcutDown)return noErr;
    host.shortcutDown=YES;
    if(host.handle) {
        host.interactionStart=NSProcessInfo.processInfo.systemUptime;
        [host trace:@"shortcut"];
        // Closing is purely native: avoid hiding/refocusing any other Wails window
        // before dismissing this one, or the compositor can show an extra frame.
        if(host.panel.visible && host.panelCentered && !host.panel.attachedSheet) bot_panel(context,0);
        else desktopEvent(host.handle,11,0,0,0);
    }
    return noErr;
}
int bot_shortcut(void *pointer,char *rawKey,int flags,int enabled) {
    BotHost *host=(__bridge BotHost *)pointer;
    NSString *key=[NSString stringWithUTF8String:rawKey];
    if(!enabled){if(host.shortcut)UnregisterEventHotKey(host.shortcut);host.shortcut=NULL;return 0;}
    if(host.shortcut && [host.shortcutKey isEqualToString:key] && host.shortcutFlags==flags)return 0;
    NSDictionary *codes=@{@"Space":@49,@"KeyA":@0,@"KeyS":@1,@"KeyD":@2,@"KeyF":@3,@"KeyH":@4,@"KeyG":@5,@"KeyZ":@6,@"KeyX":@7,@"KeyC":@8,@"KeyV":@9,@"KeyB":@11,@"KeyQ":@12,@"KeyW":@13,@"KeyE":@14,@"KeyR":@15,@"KeyY":@16,@"KeyT":@17,@"KeyO":@31,@"KeyU":@32,@"KeyI":@34,@"KeyP":@35,@"KeyL":@37,@"KeyJ":@38,@"KeyK":@40,@"KeyN":@45,@"KeyM":@46,@"Digit1":@18,@"Digit2":@19,@"Digit3":@20,@"Digit4":@21,@"Digit6":@22,@"Digit5":@23,@"Digit9":@25,@"Digit7":@26,@"Digit8":@28,@"Digit0":@29,@"F1":@122,@"F2":@120,@"F3":@99,@"F4":@118,@"F5":@96,@"F6":@97,@"F7":@98,@"F8":@100,@"F9":@101,@"F10":@109,@"F11":@103,@"F12":@111};
    NSNumber *code=codes[key]; if(!code)return paramErr;
    if(!host.shortcutHandler){
        EventTypeSpec specs[]={{kEventClassKeyboard,kEventHotKeyPressed},{kEventClassKeyboard,kEventHotKeyReleased}};
        EventHandlerRef handler=NULL;
        OSStatus result=InstallEventHandler(GetApplicationEventTarget(),bot_hotkey,2,specs,pointer,&handler);
        if(result!=noErr)return result;
        host.shortcutHandler=handler;
    }
    UInt32 mods=0;if(flags&1)mods|=controlKey;if(flags&2)mods|=optionKey;if(flags&4)mods|=shiftKey;if(flags&8)mods|=cmdKey;
    // RegisterEventHotKey alone does not reliably reject symbolic system keys.
    CFArrayRef symbolic=NULL;
    if(CopySymbolicHotKeys(&symbolic)==noErr && symbolic){
        BOOL reserved=NO;
        for(NSDictionary *entry in (__bridge NSArray *)symbolic){
            if([entry[(__bridge NSString *)kHISymbolicHotKeyEnabled] boolValue] &&
               [entry[(__bridge NSString *)kHISymbolicHotKeyCode] unsignedIntValue]==code.unsignedIntValue &&
               ([entry[(__bridge NSString *)kHISymbolicHotKeyModifiers] unsignedIntValue] & (controlKey|optionKey|shiftKey|cmdKey))==mods){reserved=YES;break;}
        }
        CFRelease(symbolic);
        if(reserved)return eventHotKeyExistsErr;
    }
    EventHotKeyRef registration=NULL;
    EventHotKeyID identifier={'Cael',host.shortcutID+1};
    OSStatus result=RegisterEventHotKey(code.unsignedIntValue,mods,identifier,GetApplicationEventTarget(),0,&registration);
    if(result!=noErr)return result;
    if(host.shortcut)UnregisterEventHotKey(host.shortcut);
    host.shortcutDown=NO;host.shortcut=registration;host.shortcutID=identifier.id;host.shortcutKey=key;host.shortcutFlags=flags;
    return 0;
}
void bot_centered_panel(void *pointer){
    BotHost *host=(__bridge BotHost *)pointer;
    if(host.panel.attachedSheet)return;
    BOOL close=host.panel.visible && host.panelCentered;
    host.panelCentered=YES;
    host.panelScreen=NSScreen.mainScreen;
    for(NSScreen *screen in NSScreen.screens){if(NSPointInRect(NSEvent.mouseLocation,screen.frame)){host.panelScreen=screen;break;}}
    bot_panel(pointer,!close);
}
void bot_panel_ready(void *pointer,int activation){
    BotHost *host=(__bridge BotHost *)pointer;
    if(activation<=0 || host.activationID!=(NSUInteger)activation || host.readyID==(NSUInteger)activation || !host.panel.visible)return;
    host.readyID=activation;
    double elapsed=host.interactionStart>0 ? (NSProcessInfo.processInfo.systemUptime-host.interactionStart)*1000 : 0;
    [host trace:[NSString stringWithFormat:@"input-ready:%.1fms",elapsed]];
}
