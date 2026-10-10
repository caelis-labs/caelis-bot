//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>
#import <CoreGraphics/CoreGraphics.h>
#import <WebKit/WebKit.h>
#import <UserNotifications/UserNotifications.h>
#import <os/log.h>
#import "native_darwin.h"
#include "bubble_layout.h"
#import "bubble_hover_darwin.h"
#import "material_darwin.h"
#import "pet_input_darwin.h"
#import "task_dock_darwin.h"
#import "capture_darwin.h"
extern void desktopEvent(uintptr_t handle, int kind, double x, double y, double scale);
extern void desktopTaskOpen(uintptr_t handle, char *identifier);
extern void desktopTaskCancelOpening(uintptr_t handle);
extern void desktopTaskUnpin(uintptr_t handle, char *identifier);
extern void desktopTaskLock(uintptr_t handle, char *identifier, int locked);
extern void desktopTaskMove(uintptr_t handle,char *identifier,char *before);
extern void desktopTaskPlace(uintptr_t handle,char *identifier,double x,double y);
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
@interface BotHotkeySlot : NSObject
@property EventHotKeyRef registration;
@property NSString *key;
@property int flags;
@property UInt32 revision;
@property BOOL down;
@end
@implementation BotHotkeySlot
@end
@interface BotInputPanel : NSPanel
@property BOOL interactive;
@end
@implementation BotInputPanel
- (BOOL)canBecomeKeyWindow { return self.interactive; }
- (BOOL)canBecomeMainWindow { return NO; }
@end
@interface BotHost : NSObject <NSMenuDelegate, UNUserNotificationCenterDelegate>
@property BotCapture *capture;
@property BOOL captureEnabled;
@property NSArray<BotHotkeySlot *> *hotkeys;
@property BotInputPanel *pet;
@property NSWindow *history;
@property BotInputPanel *bubble;
@property BOOL bubbleWanted;
@property double bubbleRequestedHeight;
@property double bubbleMaxHeight;
@property BotTaskDock *taskDock;
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
@property BOOL dragging;
@property(readonly) BOOL pressing;
@property BotPetInputView *inputView;
@property NSTimer *dragCompletion;
@property EventHandlerRef shortcutHandler;

@property BOOL visible;
@property NSStatusItem *statusItem;
@property NSDictionary<NSString *,NSString *> *language;
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
- (void)updateBubble;
- (void)collapseBubble;
- (void)trace:(NSString *)event;
- (void)observeClick:(NSEvent *)event local:(BOOL)local;
- (void)openChat:(id)sender;
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
    [self updateBubble];
    [self updateHit]; [self trace:@"drag-end"];
    desktopEvent(self.handle,1,origin.x,origin.y,0);
}
- (void)openChat:(id)sender {  desktopEvent(self.handle,2,0,0,0); }
- (void)togglePet:(id)sender { desktopEvent(self.handle,5,!self.visible,0,0); }
- (void)quit:(id)sender { desktopEvent(self.handle,7,0,0,0); }
- (void)openSettings:(id)sender {  desktopEvent(self.handle,9,0,0,0); }
- (void)checkUpdates:(id)sender {  desktopEvent(self.handle,10,0,0,0); }
- (void)singleClick {
    if (!self.visible || !self.handle) return;
    [self trace:@"single-click"];
    bot_js(self.pet,@"window.dispatchEvent(new Event('pet-touch'))");
    desktopEvent(self.handle,8,0,0,0);
}
- (void)doubleClick {
    if (!self.visible || !self.handle) return;
    [self trace:@"double-click"];
    bot_js(self.pet,@"window.dispatchEvent(new Event('pet-touch'))");
    desktopEvent(self.handle,12,0,0,0);
}
- (void)beginDrag:(NSEvent *)event {
    if (!self.visible || !self.handle) return;
    self.dragging = YES;
    self.pet.ignoresMouseEvents = NO;
    [self cancelPlane]; [self publishContext];
    [self.bubble orderOut:nil];
    [self updateBubble];
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
                if (granted) bot_notify((__bridge void *)weak, "notifications-enabled", "通知已开启", "需要你确认的事项和定时任务结果会出现在这里。", 1);
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
- (NSString *)text:(NSString *)key { return self.language[key] ?: key; }
- (NSMenu *)petMenu {
    NSMenu *menu = [NSMenu new];
    menu.autoenablesItems = NO; menu.delegate = self;
    NSMenuItem *open = [menu addItemWithTitle:[self text:@"chat"] action:@selector(openChat:) keyEquivalent:@""];
    open.target = self;
    NSMenuItem *visibility = [menu addItemWithTitle:self.visible ? [self text:@"hide"] : [self text:@"show"] action:@selector(togglePet:) keyEquivalent:@""];
    visibility.target = self; visibility.tag = 2;
    return menu;
}
- (NSMenu *)applicationMenu {
    NSMenu *menu = [self petMenu];
    [menu addItem:NSMenuItem.separatorItem];
    for(NSArray *entry in @[@[@"capture.start",NSStringFromSelector(@selector(startCapture:))],@[@"capture.paste",NSStringFromSelector(@selector(pasteCapture:))],@[@"capture.togglePins",NSStringFromSelector(@selector(toggleCapturePins:))]]) {
        NSMenuItem *item=[menu addItemWithTitle:[self text:entry[0]] action:NSSelectorFromString(entry[1]) keyEquivalent:@""];
        item.target=self;item.enabled=self.captureEnabled;
    }
    [menu addItem:NSMenuItem.separatorItem];
    NSMenuItem *settings = [menu addItemWithTitle:[self text:@"settings"] action:@selector(openSettings:) keyEquivalent:@","];
    settings.target = self;
    NSMenuItem *updates = [menu addItemWithTitle:[self text:@"updates"] action:@selector(checkUpdates:) keyEquivalent:@""];
    updates.target = self;
    [menu addItem:NSMenuItem.separatorItem];
    NSMenuItem *quit = [menu addItemWithTitle:[self text:@"quit"] action:@selector(quit:) keyEquivalent:@""];
    quit.target = self;
    return menu;
}
- (void)startCapture:(id)sender {if(self.captureEnabled)[self.capture capture];}
- (void)pasteCapture:(id)sender {if(self.captureEnabled)[self.capture paste];}
- (void)toggleCapturePins:(id)sender {if(self.captureEnabled)[self.capture togglePins];}
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
        if (item.tag == 2) item.title = self.visible ? [self text:@"hide"] : [self text:@"show"];
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
    if (!local || (event.window && (event.window==self.pet || event.window==self.history || event.window==self.bubble || event.window==self.prop))) {
        [self dismissMenu:local ? @"menu-dismiss-local" : @"menu-dismiss-global"];
    }
    if (!local || (event.window && event.window != self.bubble)) [self collapseBubble];
}
- (void)trace:(NSString *)event {
    static os_log_t logger;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ logger = os_log_create(NSBundle.mainBundle.bundleIdentifier.UTF8String ?: "dev.caelis.bot", "Desktop"); });
    if (![event hasPrefix:@"hit-"]) os_log_info(logger, "Surface event: %{public}@", event);
    NSString *path = NSProcessInfo.processInfo.environment[@"CAELIS_BOT_DESKTOP_TRACE"];
    if (!path.length) return;
    NSRect r = self.pet.frame;
    NSMutableArray *screens = [NSMutableArray new];
    for (NSScreen *s in NSScreen.screens) [screens addObject:@{@"frame":NSStringFromRect(s.frame),@"visibleFrame":NSStringFromRect(s.visibleFrame),@"scale":@(s.backingScaleFactor)}];
    NSDictionary *record = @{@"event":event,@"time":@(NSDate.date.timeIntervalSince1970),@"x":@(r.origin.x),@"y":@(r.origin.y),@"width":@(r.size.width),@"height":@(r.size.height),@"visible":@(self.pet.visible),@"petOnActiveSpace":@(self.pet.onActiveSpace),@"petOcclusionVisible":@((self.pet.occlusionState & NSWindowOcclusionStateVisible)!=0),@"inputOnActiveSpace":@(self.pet.onActiveSpace),@"singlePetSurface":@YES,@"chatVisible":@(self.history.visible),@"chatKey":@(self.history.keyWindow),@"petLevel":@(self.pet.level),@"doubleClickInterval":@(NSEvent.doubleClickInterval),@"petKey":@(self.pet.keyWindow),@"taskDockWindowNumber":@(self.taskDock.window.windowNumber),@"taskDockVisible":@(self.taskDock.window.visible),@"taskDockFrame":NSStringFromRect(self.taskDock.window.frame),@"taskCount":@(self.taskDock.count),@"bubbleVisible":@(self.bubble.visible),@"bubbleKey":@(self.bubble.keyWindow),@"bubbleFrame":NSStringFromRect(self.bubble.frame),@"frontApp":NSWorkspace.sharedWorkspace.frontmostApplication.bundleIdentifier ?: @"",@"activationPolicy":@(NSApp.activationPolicy),@"propWindowNumber":@(self.prop.windowNumber),@"propVisible":@(self.prop.visible),@"propKey":@(self.prop.keyWindow),@"propClickThrough":@(self.prop.ignoresMouseEvents),@"propFrame":NSStringFromRect(self.prop.frame),@"maskBytes":@(self.mask.length),@"screens":screens};
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
- (void)updateBubble {
    // The task entry belongs to the pet, not the chat or message bubble.
    [self.taskDock placeWithPet:self.pet.frame bounds:(self.pet.screen ?: NSScreen.mainScreen).visibleFrame visible:self.visible && !self.dragging];
    if (!self.visible || !self.bubbleWanted || self.dragging || self.history.keyWindow) {
        [(BotBubbleSurface *)self.bubble.contentView resetHover];
        if(self.bubble.visible) bot_js(self.bubble,@"window.dispatchEvent(new Event('bubble-hidden'))");
        [self.bubble orderOut:nil]; return;
    }
    NSRect pet = self.pet.frame;
    NSRect bounds = (self.pet.screen ?: NSScreen.mainScreen).visibleFrame;
    BotBubbleLayout layout=bot_bubble_layout(pet.origin.x,pet.origin.y,pet.size.width,pet.size.height,
        bounds.origin.x,bounds.origin.y,bounds.size.width,bounds.size.height,self.bubbleRequestedHeight);
    if(self.bubbleMaxHeight!=layout.maxHeight) {
        self.bubbleMaxHeight=layout.maxHeight;
        bot_js(self.bubble,[NSString stringWithFormat:@"document.documentElement.style.setProperty('--bubble-max-height','%gpx')",layout.maxHeight]);
    }
    NSRect frame=NSMakeRect(layout.x,layout.y,layout.width,layout.height);
    if(!NSEqualRects(self.bubble.frame,frame)) [self.bubble setFrame:frame display:YES];
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

void *bot_create(void *pet, void *bubble, void *history, void *prop, uintptr_t handle, unsigned char *icon, int iconLength) {
    BotHost *host = [BotHost new];
    UNUserNotificationCenter.currentNotificationCenter.delegate = host;
    [host refreshNotificationPermission];
    NSWindow *renderOwner = (__bridge NSWindow *)pet;
    host.history = (__bridge NSWindow *)history;
    host.history.backgroundColor = NSColor.windowBackgroundColor;
    host.handle = handle;
    host.scale = 1;
    host.statusItem = [NSStatusBar.systemStatusBar statusItemWithLength:NSSquareStatusItemLength];
    NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:icon length:iconLength]];
    // The full-color character has transparent breathing room of its own.
    // Fill more of the status item while retaining a margin on shorter bars.
    CGFloat iconSize = MIN(24.0, NSStatusBar.systemStatusBar.thickness - 2.0);
    image.size = NSMakeSize(iconSize,iconSize);
    host.statusItem.button.image = image;
    host.statusItem.button.toolTip = NSBundle.mainBundle.infoDictionary[@"CFBundleDisplayName"] ?: @"Caelis Bot";
    host.statusItem.button.accessibilityLabel = host.statusItem.button.toolTip;
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
    host.pet.title = renderOwner.title;
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
    BotBubbleSurface *bubbleSurface = [[BotBubbleSurface alloc] initWithFrame:NSMakeRect(0,0,360,68)];
    __weak BotHost *bubbleHost = host;
    bubbleSurface.hoverChanged = ^(BOOL inside) {
        BotHost *owner = bubbleHost;
        if (owner) bot_js(owner.bubble, [NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('bubble-hover',{detail:%@}))", inside ? @"true" : @"false"]);
    };
    bubbleContent.frame = bubbleSurface.bounds;
    bubbleContent.autoresizingMask = NSViewWidthSizable|NSViewHeightSizable;
    [bubbleSurface addSubview:bubbleContent];
    host.bubble.contentView = bubbleSurface;
    bot_install_bubble_material(host.bubble, 28, 6);
    host.bubble.title = bubbleOwner.title;
    host.bubble.opaque = NO; host.bubble.backgroundColor = NSColor.clearColor;
    host.bubble.hasShadow = NO; host.bubble.level = NSFloatingWindowLevel;
    host.bubble.hidesOnDeactivate = NO; host.bubble.releasedWhenClosed = NO;
    host.bubble.collectionBehavior = bot_space_behavior(YES);
    host.bubble.acceptsMouseMovedEvents = YES;
    NSWindow *propOwner=(__bridge NSWindow *)prop;
    host.prop=[[BotInputPanel alloc] initWithContentRect:propOwner.frame styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    NSView *propContent=propOwner.contentView;
    propOwner.contentView=[[NSView alloc] initWithFrame:propContent.bounds];
    host.prop.contentView=propContent;
    host.prop.title=propOwner.title;
    host.prop.opaque=NO;host.prop.backgroundColor=NSColor.clearColor;host.prop.hasShadow=NO;
    host.prop.level=NSFloatingWindowLevel;host.prop.hidesOnDeactivate=NO;host.prop.releasedWhenClosed=NO;
    host.prop.collectionBehavior=bot_space_behavior(YES);host.prop.ignoresMouseEvents=YES;
    __weak BotHost *weak = host;
    host.taskDock=[BotTaskDock new];
    host.taskDock.language=host.language;
    host.taskDock.notice=^(NSString *message,BOOL pending){
        NSData *data=[NSJSONSerialization dataWithJSONObject:@{@"message":message ?: @"",@"pending":@(pending)} options:0 error:nil];
        NSString *json=[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
        if(json && weak.bubble)bot_js(weak.bubble,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('terminal-notice',{detail:%@}))",json]);
    };
    host.taskDock.openTask=^(NSString *identifier){
        // The Go consumer may run immediately on another thread. Let AppKit
        // finish the nonactivating panel's mouse-up/focus transaction before
        // it asks another app to become active; never race that transaction.
        dispatch_async(dispatch_get_main_queue(),^{
            if(weak.handle)desktopTaskOpen(weak.handle,(char *)identifier.UTF8String);
        });
    };
    host.taskDock.cancelOpening=^{if(weak.handle)desktopTaskCancelOpening(weak.handle);};
    host.taskDock.unpinTask=^(NSString *identifier){if(weak.handle)desktopTaskUnpin(weak.handle,(char *)identifier.UTF8String);};
    host.taskDock.lockTask=^(NSString *identifier,BOOL locked){if(weak.handle)desktopTaskLock(weak.handle,(char *)identifier.UTF8String,locked);};
    host.taskDock.reorderTask=^(NSString *identifier,NSString *before){if(weak.handle)desktopTaskMove(weak.handle,(char *)identifier.UTF8String,(char *)before.UTF8String);};
    host.taskDock.placeTask=^(NSString *identifier,NSPoint p){if(weak.handle)desktopTaskPlace(weak.handle,(char *)identifier.UTF8String,p.x,p.y);};
    host.taskDock.gesture=^(NSString *name){
        if([name isEqualToString:@"attention"]) {
            bot_js(weak.history,@"window.dispatchEvent(new Event('task-dock-expanded'))");
        }
        if(weak.handle)bot_gesture((__bridge void *)weak,(char *)name.UTF8String);
    };
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
    view.accessibilityLabel = renderOwner.title;
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
    id deactivate = [NSNotificationCenter.defaultCenter addObserverForName:NSApplicationDidResignActiveNotification object:nil queue:nil usingBlock:^(NSNotification *note) { [weak.inputView cancelInteraction]; [weak dismissMenu:@"menu-dismiss-deactivate"]; [weak collapseBubble]; [weak updateBubble]; }];
    [host.observers addObject:deactivate];
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
    for (NSWindow *window in @[host.pet,host.bubble]) {
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
                    // Switching desktops preserves placement and hidden preference.
                    [weak collapseBubble];
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
    } else {  [host.pet orderOut:nil]; [host.bubble orderOut:nil]; }
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
int bot_prepare_window_recall(void *pointer) {
    BotHost *host = (__bridge BotHost *)pointer;
    [host.inputView cancelInteraction];
    if (host.history.attachedSheet) {
        [NSApp activateIgnoringOtherApps:YES];
        [host.history.attachedSheet makeKeyAndOrderFront:nil];
        return 0;
    }
    [host collapseBubble];
    return 1;
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
    [host.taskDock stop];
    host.handle = 0;
    for(BotHotkeySlot *slot in host.hotkeys)if(slot.registration)UnregisterEventHotKey(slot.registration);
    if(host.shortcutHandler) RemoveEventHandler(host.shortcutHandler);
    if (host.globalMonitor) [NSEvent removeMonitor:host.globalMonitor];
    if (host.localMonitor) [NSEvent removeMonitor:host.localMonitor];
    for (id observer in host.observers) {
        [NSNotificationCenter.defaultCenter removeObserver:observer];
        [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:observer];
    }
    [NSStatusBar.systemStatusBar removeStatusItem:host.statusItem];
    [host.dragCompletion invalidate];

    [host.pet close]; [host.prop close]; [host.bubble close];
}

void bot_expand_bubble(void *pointer,int expanded) {
 BotHost *host=(__bridge BotHost *)pointer;
 if(!expanded){[host collapseBubble];return;}
 host.bubble.interactive=YES; host.bubbleWanted=YES;
 [host updateBubble];
 // Only this explicit action enables keyboard entry. Arrival stays non-key.
 [host.bubble makeKeyAndOrderFront:nil];
 bot_js(host.bubble,@"window.dispatchEvent(new Event('bubble-expand'))");
}
void bot_bubble_height(void *pointer,int height) {
 BotHost *host=(__bridge BotHost *)pointer;
 host.bubbleRequestedHeight=height;
 // A reloaded renderer may have lost its CSS variable without changing the
 // display's geometry. Re-send the limit when it reports its content height.
 host.bubbleMaxHeight=0;
 [host updateBubble];
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
    if (host.history.keyWindow || (host.visible && !reminder)) return;
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

void bot_dismiss_notification(char *identifier) {
    NSString *key = [NSString stringWithUTF8String:identifier];
    if (!key.length) return;
    [UNUserNotificationCenter.currentNotificationCenter removeDeliveredNotificationsWithIdentifiers:@[key]];
    [UNUserNotificationCenter.currentNotificationCenter removePendingNotificationRequestsWithIdentifiers:@[key]];
}

int bot_notification_status(void *pointer) {
    BotHost *host = (__bridge BotHost *)pointer;
    // Called from a Go worker. A focus-triggered read must return the new OS
    // state, not the cached value from before the asynchronous callback.
    if (NSThread.isMainThread) return -1;
    dispatch_semaphore_t done = dispatch_semaphore_create(0);
    __block int status = -1;
    [UNUserNotificationCenter.currentNotificationCenter getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *settings) {
        dispatch_async(dispatch_get_main_queue(), ^{
            host.notificationPermission = settings.authorizationStatus;
            status = host.notificationError ? -1 : (int)settings.authorizationStatus;
            dispatch_semaphore_signal(done);
        });
    }];
    if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC))) return -1;
    return status;
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
    bot_install_material(window, 16, 190, 0);
}

void bot_activity(void *pointer, char *activity) {
    BotHost *host = (__bridge BotHost *)pointer;
    NSString *state = [NSString stringWithUTF8String:activity];
    if (![@[@"idle",@"working",@"waiting",@"dreaming"] containsObject:state]) return;
    [host updateBubble];
    if (![state isEqualToString:@"idle"]) [host cancelPlane];
    bot_js(host.pet,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('pet-activity',{detail:'%@'}))",state]);
}

void bot_tasks(void *pointer,char *json) {
    BotHost *host=(__bridge BotHost *)pointer;
    NSData *data=[[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    id tasks=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    [host.taskDock setTasks:[tasks isKindOfClass:NSArray.class] ? tasks : @[]];
    [host updateBubble]; [host trace:@"task-bubbles-update"];
}
void bot_task_failure(void *pointer,char *message) {
    [((__bridge BotHost *)pointer).taskDock showFailure:[NSString stringWithUTF8String:message]];
}
void bot_task_opening(void *pointer,char *identifier,char *message) {
    [((__bridge BotHost *)pointer).taskDock setOpening:[NSString stringWithUTF8String:identifier] message:[NSString stringWithUTF8String:message]];
}

// Carbon hotkeys are system registrations; no keyboard surveillance permission.
static OSStatus bot_hotkey(EventHandlerCallRef next, EventRef event, void *context) {
    BotHost *host=(__bridge BotHost *)context;
    EventHotKeyID key;
    if(GetEventParameter(event,kEventParamDirectObject,typeEventHotKeyID,NULL,sizeof(key),NULL,&key)!=noErr || key.signature!='Cael')return eventNotHandledErr;
    NSUInteger kind=key.id>>28;if(kind>=host.hotkeys.count)return eventNotHandledErr;
    BotHotkeySlot *slot=host.hotkeys[kind];
    if(!slot.registration||(key.id&0x0fffffff)!=slot.revision)return eventNotHandledErr;
    BOOL down=GetEventKind(event)!=kEventHotKeyReleased;
    if(slot.down==down)return noErr;slot.down=down;
    if(kind==1){[host.taskDock setShortcutHeld:down];return noErr;}
    if(!down)return noErr;
    if(kind==2){if(host.captureEnabled)[host.capture capture];return noErr;}
    if(kind==3){if(host.captureEnabled)[host.capture paste];return noErr;}
    if(host.handle){[host trace:@"shortcut"];desktopEvent(host.handle,11,0,0,0);}
    return noErr;
}
void bot_toggle_tasks(void *pointer){[((__bridge BotHost *)pointer).taskDock toggle];}
int bot_shortcut(void *pointer,char *rawKey,int flags,int enabled,int kind) {
    BotHost *host=(__bridge BotHost *)pointer;
    if(kind<0||kind>3)return paramErr;
    if(!host.hotkeys)host.hotkeys=@[[BotHotkeySlot new],[BotHotkeySlot new],[BotHotkeySlot new],[BotHotkeySlot new]];
    BotHotkeySlot *slot=host.hotkeys[kind];NSString *key=[NSString stringWithUTF8String:rawKey];
    if(!enabled){if(slot.registration)UnregisterEventHotKey(slot.registration);slot.registration=NULL;slot.down=NO;if(kind==1)[host.taskDock setShortcutHeld:NO];return noErr;}
    if(slot.registration&&[slot.key isEqual:key]&&slot.flags==flags)return noErr;
    for(BotHotkeySlot *other in host.hotkeys)if(other!=slot&&other.registration&&[other.key isEqual:key]&&other.flags==flags)return eventHotKeyExistsErr;
    NSDictionary *codes=@{@"Space":@49,@"KeyA":@0,@"KeyS":@1,@"KeyD":@2,@"KeyF":@3,@"KeyH":@4,@"KeyG":@5,@"KeyZ":@6,@"KeyX":@7,@"KeyC":@8,@"KeyV":@9,@"KeyB":@11,@"KeyQ":@12,@"KeyW":@13,@"KeyE":@14,@"KeyR":@15,@"KeyY":@16,@"KeyT":@17,@"KeyO":@31,@"KeyU":@32,@"KeyI":@34,@"KeyP":@35,@"KeyL":@37,@"KeyJ":@38,@"KeyK":@40,@"KeyN":@45,@"KeyM":@46,@"Digit1":@18,@"Digit2":@19,@"Digit3":@20,@"Digit4":@21,@"Digit6":@22,@"Digit5":@23,@"Digit9":@25,@"Digit7":@26,@"Digit8":@28,@"Digit0":@29,@"F1":@122,@"F2":@120,@"F3":@99,@"F4":@118,@"F5":@96,@"F6":@97,@"F7":@98,@"F8":@100,@"F9":@101,@"F10":@109,@"F11":@103,@"F12":@111};
    NSNumber *code=codes[key];if(!code)return paramErr;
    if(!host.shortcutHandler){
        EventTypeSpec specs[]={{kEventClassKeyboard,kEventHotKeyPressed},{kEventClassKeyboard,kEventHotKeyReleased}};
        EventHandlerRef handler=NULL;OSStatus result=InstallEventHandler(GetApplicationEventTarget(),bot_hotkey,2,specs,pointer,&handler);
        if(result!=noErr)return result;host.shortcutHandler=handler;
    }
    UInt32 mods=0;if(flags&1)mods|=controlKey;if(flags&2)mods|=optionKey;if(flags&4)mods|=shiftKey;if(flags&8)mods|=cmdKey;
    CFArrayRef symbolic=NULL;
    if(CopySymbolicHotKeys(&symbolic)==noErr&&symbolic){
        BOOL reserved=NO;for(NSDictionary *entry in (__bridge NSArray *)symbolic){
            if([entry[(__bridge NSString *)kHISymbolicHotKeyEnabled] boolValue]&&[entry[(__bridge NSString *)kHISymbolicHotKeyCode] unsignedIntValue]==code.unsignedIntValue&&([entry[(__bridge NSString *)kHISymbolicHotKeyModifiers] unsignedIntValue]&(controlKey|optionKey|shiftKey|cmdKey))==mods){reserved=YES;break;}}
        CFRelease(symbolic);if(reserved)return eventHotKeyExistsErr;
    }
    UInt32 revision=(slot.revision+1)&0x0fffffff;
    EventHotKeyRef registration=NULL;EventHotKeyID identifier={'Cael',((UInt32)kind<<28)|revision};
    OSStatus result=RegisterEventHotKey(code.unsignedIntValue,mods,identifier,GetApplicationEventTarget(),0,&registration);
    if(result!=noErr)return result;
    if(slot.registration)UnregisterEventHotKey(slot.registration);
    if(kind==1)[host.taskDock setShortcutHeld:NO];
    slot.registration=registration;slot.revision=revision;slot.key=key;slot.flags=flags;slot.down=NO;return noErr;
}
void bot_bind_capture(void *pointer,void *capture) {
    BotHost *host=(__bridge BotHost *)pointer;host.capture=(__bridge BotCapture *)capture;
    host.captureEnabled=YES;
    NSMutableArray *excluded=[NSMutableArray new];
    for(id window in @[host.pet?:NSNull.null,host.bubble?:NSNull.null,host.prop?:NSNull.null,host.taskDock.window?:NSNull.null])if([window isKindOfClass:NSWindow.class])[excluded addObject:window];
    host.capture.excludedWindows=excluded;
    host.capture.language=host.language;
}
void bot_capture_enabled(void *pointer,int enabled) {
    BotHost *host=(__bridge BotHost *)pointer;
    host.captureEnabled=enabled!=0;
    host.capture.enabled=enabled!=0;
}
char *bot_preferred_languages(void) {
    @autoreleasepool {
        NSData *data = [NSJSONSerialization dataWithJSONObject:NSLocale.preferredLanguages options:0 error:nil];
        return strdup([[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String);
    }
}
void bot_language(void *pointer, char *json) {
    BotHost *host = (__bridge BotHost *)pointer;
    NSData *data = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    host.language = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    host.taskDock.language = host.language;
    host.capture.language = host.language;
    // These panels own the visible content; their Wails source windows are hidden.
    host.pet.title = [host text:@"petTitle"];
    host.bubble.title = [host text:@"bubbleTitle"];
    host.prop.title = [host text:@"propTitle"];
    host.inputView.accessibilityLabel = [host text:@"petLabel"];
    host.inputView.accessibilityHelp = [host text:@"petActionHint"];
    host.statusItem.menu = [host applicationMenu];
}

void bot_task_snapshot(void *pointer,const char *identifier,const char *source) {
    NSData *data=[[NSString stringWithUTF8String:source] dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *value=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    if([value isKindOfClass:NSDictionary.class])[((__bridge BotHost *)pointer).taskDock captureTask:[NSString stringWithUTF8String:identifier] source:value];
}

void bot_task_transitions(void *pointer,char *data) {
 NSData *json=[[NSString stringWithUTF8String:data] dataUsingEncoding:NSUTF8StringEncoding];
 NSDictionary *value=[NSJSONSerialization JSONObjectWithData:json options:0 error:nil];
 if([value isKindOfClass:NSDictionary.class])[((__bridge BotHost *)pointer).taskDock setTransitions:value];
}
