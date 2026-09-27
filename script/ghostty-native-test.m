#import "ghostty_darwin.h"
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
