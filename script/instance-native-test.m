#import <Cocoa/Cocoa.h>
#include <assert.h>
// The disposable bundle is intentionally not installed/registered system-wide.
#define BOT_INSTANCE_APPLICATION_URL(bundle) NSBundle.mainBundle.bundleURL
#include "instance_darwin.m"

// Two disposable GUI instances, two windows each. Tests the production bridge
// through LaunchServices and NSRunningApplication, not terminal scripts or AX.
@interface FixtureDelegate : NSObject <NSApplicationDelegate>
@property NSMutableArray *windows;
@property BOOL confirmQuit;
@property int quitRequests;
@property NSString *confirmationPath;
@end
@implementation FixtureDelegate
- (void)application:(NSApplication *)app openFiles:(NSArray<NSString *> *)files {
    if([files.firstObject.lastPathComponent isEqual:@"cancel-open.command"]) {
        [app replyToOpenOrPrint:NSApplicationDelegateReplyCancel];return;
    }
    if([files.firstObject.lastPathComponent isEqual:@"skip-open.command"]) {
        // Like applications that return success after cancelling their own UI.
        [app replyToOpenOrPrint:NSApplicationDelegateReplySuccess];return;
    }
    self.confirmQuit=[files.firstObject.lastPathComponent isEqual:@"confirm.command"];
    self.confirmationPath=self.confirmQuit ? files.firstObject : nil;
    if (!self.windows) self.windows = [NSMutableArray array];
    for (int i=(int)self.windows.count;i<2;i++) {
        NSWindow *window = [[NSWindow alloc] initWithContentRect:NSMakeRect(80+i*80,100+i*60,380,160)
            styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskMiniaturizable
            backing:NSBackingStoreBuffered defer:NO];
        window.title = @"Caelis Bot disposable instance fixture"; window.releasedWhenClosed=NO;
        [self.windows addObject:window]; [window makeKeyAndOrderFront:nil];
    }
    [app replyToOpenOrPrint:NSApplicationDelegateReplySuccess];
    [[NSString stringWithFormat:@"%d:%lu",getpid(),(unsigned long)self.windows.count] writeToFile:[files.firstObject stringByAppendingString:@".opened"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
    if(self.confirmQuit) [@"ready" writeToFile:[self.confirmationPath stringByAppendingString:@".ready"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)app {
    if(!self.confirmQuit) return NSTerminateNow;
    if(++self.quitRequests==1) return NSTerminateCancel;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,6*NSEC_PER_SEC),dispatch_get_main_queue(),^{[app replyToApplicationShouldTerminate:YES];});
    return NSTerminateLater;
}
@end
static void pump(int (^ready)(void)) {
    NSDate *deadline=[NSDate dateWithTimeIntervalSinceNow:8];
    while (!ready()) {
        assert(deadline.timeIntervalSinceNow>0);
        [NSThread sleepForTimeInterval:0.02];
    }
}
static int observed(void *handle) {
    int state,pid; uint64_t birth;
    int result=bot_terminal_instance_observe(handle,&state,&pid,&birth);
    assert(result>=0); if(result==0) return 0;
    return state;
}
static void runFixture(void) {
    NSString *bundle=NSBundle.mainBundle.bundleIdentifier;
    NSString *script=[[NSBundle.mainBundle.bundlePath stringByDeletingLastPathComponent] stringByAppendingPathComponent:@"fixture.command"];
    assert(script.length);
    puts("launch first");void *first=bot_terminal_instance_open(bundle.UTF8String,script.UTF8String);
    pump(^{return bot_terminal_instance_ready(first)!=0;});
    assert(bot_terminal_instance_ready(first)==1);
    puts("launch second");void *second=bot_terminal_instance_open(bundle.UTF8String,script.UTF8String);
    pump(^{return bot_terminal_instance_ready(second)!=0;});
    assert(bot_terminal_instance_ready(second)==1);
    BotTerminalInstance *a=(__bridge BotTerminalInstance *)first,*b=(__bridge BotTerminalInstance *)second;
    assert(a.app.processIdentifier!=b.app.processIdentifier && a.app.processIdentifier!=getpid());
    pump(^{return bot_terminal_instance_document_result(first)!=1;});
    assert(bot_terminal_instance_document_result(first)==2);
    NSString *cancelPath=[[script stringByDeletingLastPathComponent] stringByAppendingPathComponent:@"cancel-open.command"];
    NSString *skipPath=[[script stringByDeletingLastPathComponent] stringByAppendingPathComponent:@"skip-open.command"];
    for(NSString *path in @[cancelPath,skipPath]) [@"fixture" writeToFile:path atomically:YES encoding:NSUTF8StringEncoding error:nil];
    assert(bot_terminal_instance_open_document(first,cancelPath.UTF8String)==1);
    pump(^{return bot_terminal_instance_document_result(first)!=1;});
    printf("document cancellation reply=%d\n",bot_terminal_instance_document_result(first));
    assert(bot_terminal_instance_document_result(first)==3);
    assert(bot_terminal_instance_open_document(first,skipPath.UTF8String)==1);
    pump(^{return bot_terminal_instance_document_result(first)!=1;});
    assert(bot_terminal_instance_document_result(first)==2);
    NSString *reusePath=[[script stringByDeletingLastPathComponent] stringByAppendingPathComponent:@"reuse.command"];
    [@"fixture" writeToFile:reusePath atomically:YES encoding:NSUTF8StringEncoding error:nil];
    assert(bot_terminal_instance_open_document(first,reusePath.UTF8String)==1);
    pump(^{return bot_terminal_instance_document_result(first)!=1;});
    NSString *receipt=[NSString stringWithContentsOfFile:[reusePath stringByAppendingString:@".opened"] encoding:NSUTF8StringEncoding error:nil];
    assert(([receipt isEqual:[NSString stringWithFormat:@"%d:2",a.app.processIdentifier]]));
    // An existing application returned by LaunchServices is never adopted.
    BotTerminalInstance *reused=[BotTerminalInstance new]; reused.bundle=bundle;
    assert(!instance_accept(reused,a.app,[NSSet setWithObject:@(a.app.processIdentifier)]));
    pump(^{return (int)(a.app.finishedLaunching && b.app.finishedLaunching);});
    for (int cycle=0;cycle<3;cycle++) {
        printf("cycle %d hide/show\n",cycle);
        int hideResult=bot_terminal_instance_apply(first,3);printf("hide result=%d state=%d finished=%d policy=%ld\n",hideResult,observed(first),a.app.finishedLaunching,(long)a.app.activationPolicy);assert(hideResult==1);pump(^{return observed(first)==2;});
        assert(!b.app.hidden && !b.app.terminated);
        int showResult=bot_terminal_instance_apply(first,2);printf("show result=%d\n",showResult);assert(showResult==1);
        pump(^{return observed(first)==4;});
    }
    uint64_t saved=a.birth;a.birth++;
    assert(observed(first)==1 && bot_terminal_instance_apply(first,1)==-1 && bot_terminal_instance_open_document(first,script.UTF8String)==-1);
    a.birth=saved;assert(!a.app.terminated);
    puts("quit first");assert(bot_terminal_instance_apply(first,1)==1); pump(^{return observed(first)==1;});
    assert(!b.app.terminated);
    bot_terminal_instance_release(first);
    bot_terminal_instance_release(second);
    assert(!b.app.terminated); // Detaching a card never quits its old GUI.
    void *cleanup=bot_terminal_instance_adopt(b.app.processIdentifier,bundle.UTF8String);
    assert(bot_terminal_instance_apply(cleanup,1)==1);pump(^{return observed(cleanup)==1;});
    bot_terminal_instance_release(cleanup);
    // Explicitly open again after confirmed exit; no prior instance is reused.
    void *reopened=bot_terminal_instance_open(bundle.UTF8String,script.UTF8String);
    pump(^{return bot_terminal_instance_ready(reopened)!=0;});assert(bot_terminal_instance_ready(reopened)==1);
    assert(bot_terminal_instance_apply(reopened,1)==1);pump(^{return observed(reopened)==1;});
    bot_terminal_instance_release(reopened);
    NSString *confirmPath=[[script stringByDeletingLastPathComponent] stringByAppendingPathComponent:@"confirm.command"];
    [@"fixture" writeToFile:confirmPath atomically:YES encoding:NSUTF8StringEncoding error:nil];
    void *confirmation=bot_terminal_instance_open(bundle.UTF8String,confirmPath.UTF8String);
    pump(^{return bot_terminal_instance_ready(confirmation)!=0;});assert(bot_terminal_instance_ready(confirmation)==1);
    pump(^{return (int)[NSFileManager.defaultManager fileExistsAtPath:[confirmPath stringByAppendingString:@".ready"]];});
    assert(bot_terminal_instance_request_close(confirmation)==1);
    pump(^{return bot_terminal_instance_close_result(confirmation)!=1;});
    printf("quit cancellation reply=%d\n",bot_terminal_instance_close_result(confirmation));
    assert(bot_terminal_instance_close_result(confirmation)==3 && observed(confirmation)!=1);
    assert(bot_terminal_instance_request_close(confirmation)==1);
    // A repeated gesture while the native confirmation is pending must not
    // send a second request. Confirmation intentionally exceeds the old 4s limit.
    assert(bot_terminal_instance_request_close(confirmation)==1);
    pump(^{return observed(confirmation)==1;});
    bot_terminal_instance_release(confirmation);
    puts("PASS instance ownership, multi-window group hiding, foreground restore, normal quit, reopen, detach without quit, foreign-instance isolation, birth fence");
    puts("PASS standard quit cancellation, delayed confirmation, pending request deduplication");
    puts("PASS standard document cancellation, success without execution, same-process/window reuse and birth fence");
    [@"passed" writeToFile:[script stringByAppendingString:@".result"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
    exit(0);
}
int main(int argc,const char **argv) { @autoreleasepool {
    setbuf(stdout,NULL);[NSApplication sharedApplication]; [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    FixtureDelegate *delegate=[FixtureDelegate new];NSApp.delegate=delegate;
    if(argc>1 && strcmp(argv[1],"--controller")==0) dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED,0),^{runFixture();});
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,30*NSEC_PER_SEC),dispatch_get_main_queue(),^{[NSApp terminate:nil];});
    [NSApp run]; return 0;
}}
