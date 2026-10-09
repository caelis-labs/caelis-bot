//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import "updater_darwin.h"

// A narrow, dynamically loaded Sparkle 2 API. No SDK or framework is needed to
// compile portable Go tests. The bundle carries the SHA-256-pinned framework.
// These selectors are checked by the native integration test against that SDK.
@protocol BotSparkleUpdater <NSObject>
@property(nonatomic) BOOL automaticallyChecksForUpdates;
@property(nonatomic, readonly) BOOL canCheckForUpdates;
- (BOOL)startUpdater:(NSError **)error;
- (void)resetUpdateCycleAfterShortDelay;
@end
@protocol BotSparkleController <NSObject>
- (instancetype)initWithStartingUpdater:(BOOL)start updaterDelegate:(id)delegate userDriverDelegate:(id)userDelegate;
@property(nonatomic, readonly) id<BotSparkleUpdater> updater;
- (void)checkForUpdates:(id)sender;
@end

extern void botUpdaterPrepare(void);
extern void botUpdaterAborted(void);
@interface BotUpdateDelegate : NSObject
@property(nonatomic, copy) void (^installHandler)(void);
@property(nonatomic, strong) NSTimer *timer;
@end
@implementation BotUpdateDelegate
- (NSString *)feedURLStringForUpdater:(id)updater {
    NSBundle *host = NSBundle.mainBundle;
    NSString *key = @"CaelisUpdateChannel";
    NSString *selected = [[NSUserDefaults standardUserDefaults] stringForKey:key];
    if (!selected) selected = host.infoDictionary[@"CaelisDefaultUpdateChannel"];
    if ([selected isEqualToString:@"dev"]) return host.infoDictionary[@"CaelisDevFeedURL"];
    return host.infoDictionary[@"SUFeedURL"];
}
- (BOOL)updater:(id)updater shouldPostponeRelaunchForUpdate:(id)item untilInvokingBlock:(void (^)(void))handler {
    self.installHandler = handler;
    [self.timer invalidate];
    self.timer = [NSTimer scheduledTimerWithTimeInterval:2 repeats:YES block:^(NSTimer *timer) { botUpdaterPrepare(); }];
    // Go drains owned resources off the AppKit thread; the install handler runs
    // only after user and schedule admission have been frozen and Close finishes.
    botUpdaterPrepare();
    return YES;
}
- (void)updater:(id)updater didAbortWithError:(NSError *)error {
    [self.timer invalidate]; self.timer = nil; self.installHandler = nil;
    botUpdaterAborted();
}
@end

static id<BotSparkleController> controller;
static BotUpdateDelegate *updateDelegate;
int bot_updater_start(void) {
    if (controller) return 1;
    NSBundle *host = NSBundle.mainBundle;
    if (![host.infoDictionary[@"CaelisAutoUpdatesEnabled"] boolValue]) return 0;
    NSBundle *framework = [NSBundle bundleWithPath:[host.privateFrameworksPath stringByAppendingPathComponent:@"Sparkle.framework"]];
    NSError *error = nil;
    if (![framework loadAndReturnError:&error]) { NSLog(@"Updater framework unavailable: %@", error.localizedDescription); return -1; }
    Class cls = NSClassFromString(@"SPUStandardUpdaterController");
    if (!cls || ![cls instancesRespondToSelector:@selector(initWithStartingUpdater:updaterDelegate:userDriverDelegate:)]) return -1;
    updateDelegate = [BotUpdateDelegate new];
    controller = [(id<BotSparkleController>)[cls alloc] initWithStartingUpdater:NO updaterDelegate:updateDelegate userDriverDelegate:nil];
    if (![controller.updater startUpdater:&error]) {
        NSLog(@"Updater could not start: %@", error.localizedDescription);
        controller = nil; updateDelegate = nil; return -1;
    }
    return 1;
}
int bot_updater_check(void) {
    if (!controller) return 0;
    if (!controller.updater.canCheckForUpdates) return -1;
    [NSApp activateIgnoringOtherApps:YES];
    [controller checkForUpdates:nil];
    return 1;
}
bool bot_updater_automatic(void) { return controller.updater.automaticallyChecksForUpdates; }
void bot_updater_set_automatic(bool enabled) { controller.updater.automaticallyChecksForUpdates = enabled; }
bool bot_updater_dev_channel(void) {
    NSString *selected = [[NSUserDefaults standardUserDefaults] stringForKey:@"CaelisUpdateChannel"];
    if (!selected) selected = NSBundle.mainBundle.infoDictionary[@"CaelisDefaultUpdateChannel"];
    return [selected isEqualToString:@"dev"];
}
bool bot_updater_set_dev_channel(bool enabled) {
    if (!controller || !controller.updater.canCheckForUpdates || updateDelegate.installHandler) return false;
    [[NSUserDefaults standardUserDefaults] setObject:(enabled ? @"dev" : @"stable") forKey:@"CaelisUpdateChannel"];
    [controller.updater resetUpdateCycleAfterShortDelay];
    return true;
}
bool bot_updater_waiting(void) { return updateDelegate.installHandler != nil; }
bool bot_updater_claim(void) {
    if (!updateDelegate.installHandler) return false;
    [updateDelegate.timer invalidate]; updateDelegate.timer = nil;
    return true;
}
bool bot_updater_finish(void) {
    void (^handler)(void) = updateDelegate.installHandler;
    [updateDelegate.timer invalidate]; updateDelegate.timer = nil; updateDelegate.installHandler = nil;
    if (!handler) return false;
    handler();
    return true;
}
void bot_updater_fail(const char *message) {
    [updateDelegate.timer invalidate]; updateDelegate.timer = nil;
    // Never invoke a postponed install after a failed durability or shutdown
    // handoff. The failure is shown immediately, even if Settings is closed.
    updateDelegate.installHandler = nil;
    NSAlert *alert = [NSAlert new];
    BOOL chinese = [[NSLocale preferredLanguages].firstObject hasPrefix:@"zh"];
    alert.messageText = chinese ? @"Caelis Bot 更新未能继续" : @"Caelis Bot could not continue the update";
    alert.informativeText = message ? [NSString stringWithUTF8String:message] : (chinese ? @"请检查应用状态后重试。" : @"Check the app status and try again.");
    [NSApp activateIgnoringOtherApps:YES];
    [alert runModal];
}
void bot_updater_quit_after_failure(void) { [NSApp terminate:nil]; }
void bot_updater_stop(void) {
    [updateDelegate.timer invalidate]; updateDelegate.timer = nil; updateDelegate.installHandler = nil;
    controller = nil; updateDelegate = nil;
}
