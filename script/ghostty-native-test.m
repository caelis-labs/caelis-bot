#include "ghostty_darwin.m"
#include <assert.h>
#include <unistd.h>
#import <ApplicationServices/ApplicationServices.h>

// Decode the launch dictionary locally. No external terminal or TCC request.
static NSDictionary *received;
@interface FixtureNewWindow : NSScriptCommand
@end

@interface FixtureApplication : NSObject
@property(getter=isHidden) BOOL hidden;
@property(getter=isTerminated) BOOL terminated;
@property NSUInteger activations;
@property NSUInteger unhides;
@property NSUInteger hides;
- (BOOL)hide;
- (BOOL)unhide;
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options;
@end
@implementation FixtureApplication
- (BOOL)hide { self.hidden=YES;self.hides++;return YES; }
- (BOOL)unhide { self.hidden=NO;self.unhides++;return YES; }
- (BOOL)activateWithOptions:(NSApplicationActivationOptions)options {
    assert(!self.hidden && !self.terminated);
    assert(options==NSApplicationActivateIgnoringOtherApps);
    self.activations++;return YES;
}
@end
@implementation FixtureNewWindow
- (id)performDefaultImplementation {
    received = self.evaluatedArguments[@"configuration"];
    return nil;
}
@end

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Simulate a submitted launch whose completion arrives after the Go
        // caller cancels at PID zero. Only this fixture's own process is used;
        // cancellation must suppress every creation/activation Apple Event.
        NSRunningApplication *selfApp=NSRunningApplication.currentApplication;
        BotGhosttyLaunch *pending=[BotGhosttyLaunch new];
        pending.instance=(__bridge_transfer id)bot_terminal_instance_pending(NSBundle.mainBundle.bundleIdentifier.UTF8String);
        void *launchLease=(__bridge_retained void *)pending;
        void *owner=task_ghostty_open_instance(launchLease);
        int pid=0,state=0;long launchError=0;uint64_t birth=0;
        assert(task_ghostty_open_poll(launchLease,&pid,&launchError)==0 && pid==0);
        task_ghostty_open_release(launchLease);
        assert(pending.cancelled);
        assert(bot_terminal_instance_observe(owner,&state,&pid,&birth)==0);
        task_ghostty_completed(pending,selfApp,nil,[NSSet set],@"must not execute");
        pending=nil; // The caller's retained owner must survive launch-state release.
        assert(bot_terminal_instance_observe(owner,&state,&pid,&birth)==1);
        assert(pid==getpid() && birth>0 && state!=1);
        bot_terminal_instance_release(owner);
        void *unsubmitted=bot_terminal_instance_pending(NSBundle.mainBundle.bundleIdentifier.UTF8String);
        bot_terminal_instance_not_submitted(unsubmitted);
        assert(bot_terminal_instance_observe(unsubmitted,&state,&pid,&birth)==1 && state==1);
        bot_terminal_instance_release(unsubmitted);
        puts("Ghostty cancelled-before-PID retains late native owner; unsubmitted launch is distinguishable (no external events)");
        NSWorkspaceOpenConfiguration *launch = task_ghostty_configuration();
        assert(launch.createsNewApplicationInstance && !launch.allowsRunningApplicationSubstitution);
        assert(!launch.activates && !launch.addsToRecentItems);
        assert(([launch.arguments isEqual:@[@"--initial-window=false", @"--window-save-state=never", @"--quit-after-last-window-closed=true"]]));
        FixtureApplication *app=[FixtureApplication new];
        assert(task_ghostty_show((NSRunningApplication *)app));
        assert(app.activations==1 && app.unhides==0);
        app.terminated=YES;
        assert(!task_ghostty_show((NSRunningApplication *)app));
        assert(app.activations==1);
        NSString *command = @"/bin/sh '/tmp/private ''quoted'' $(not-executed)/Caelis Bot.command'";
        NSAppleEventDescriptor *event = task_ghostty_create_event(getpid(), command);
        assert([event attributeDescriptorForKeyword:keyEventClassAttr].typeCodeValue == 'Ghst');
        assert([event attributeDescriptorForKeyword:keyEventIDAttr].typeCodeValue == 'NWin');
        assert([event attributeDescriptorForKeyword:keyAddressAttr].descriptorType == typeKernelProcessID);
        assert([[event paramDescriptorForKeyword:'GNwS'] descriptorForKeyword:'GScW'].booleanValue == NO);

        NSURL *url = [NSBundle.mainBundle.resourceURL URLByAppendingPathComponent:@"Fixture.sdef"];
        NSXMLDocument *dictionary = [[NSXMLDocument alloc] initWithContentsOfURL:url options:0 error:nil];
        assert(task_ghostty_dictionary_supports_window(dictionary));
        assert(!task_ghostty_dictionary_supports_window(nil));
        NSXMLNode *property = [dictionary nodesForXPath:@"//property[@code='GScC']" error:nil].firstObject;
        [property detach];
        assert(!task_ghostty_dictionary_supports_window(dictionary));

        [[NSScriptSuiteRegistry sharedScriptSuiteRegistry] loadSuitesFromBundle:NSBundle.mainBundle];
        AppleEvent reply;
        assert(AECreateDesc(typeAppleEvent, NULL, 0, &reply) == noErr);
        OSErr error = [[NSAppleEventManager sharedAppleEventManager] dispatchRawAppleEvent:event.aeDesc withRawReply:&reply handlerRefCon:(SRefCon)1];
        AEDisposeDesc(&reply);
        assert(error == noErr);
        assert([received isKindOfClass:NSDictionary.class]);
        assert([received[@"command"] isEqual:command]);
        assert([received[@"waitAfterCommand"] isEqual:@NO]);
        puts("Ghostty native contract: single window route, PID target, Cocoa configuration decode passed (no external events)");
    }
    return 0;
}
