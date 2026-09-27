//go:build darwin && cgo

#import "ghostty_darwin.h"
#import "instance_darwin.h"

// A launch has exactly one creation event. The callback/worker retain the state
// after cancellation; releasing the caller's reference never leaves dangling data.
@interface BotGhosttyLaunch : NSObject
@property(nonatomic) BOOL cancelled;
@property(nonatomic) int status;
@property(nonatomic) pid_t pid;
@property(nonatomic) long error;
@property(nonatomic) id instance;
@end
@implementation BotGhosttyLaunch
@end

NSWorkspaceOpenConfiguration *task_ghostty_configuration(void) {
    NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
    config.createsNewApplicationInstance = YES;
    config.allowsRunningApplicationSubstitution = NO;
    config.activates = NO;
    config.addsToRecentItems = NO;
    // No document URLs or executable arguments: launch quietly, then create one
    // configured window. Restoring or activating an empty app can create a shell.
    config.arguments = @[@"--initial-window=false", @"--window-save-state=never",
                         @"--quit-after-last-window-closed=true"];
    return config;
}

BOOL task_ghostty_show(NSRunningApplication *app) {
    // Creating the application must stay inactive (no empty shell), but a
    // successfully created task window is an explicit foreground request.
    if (app.terminated) return NO;
    return [app activateWithOptions:NSApplicationActivateIgnoringOtherApps];
}

NSAppleEventDescriptor *task_ghostty_create_event(pid_t pid, NSString *command) {
    NSAppleEventDescriptor *config = [NSAppleEventDescriptor recordDescriptor];
    [config setDescriptor:[NSAppleEventDescriptor descriptorWithString:command] forKeyword:'GScC'];
    [config setDescriptor:[NSAppleEventDescriptor descriptorWithBoolean:NO] forKeyword:'GScW'];
    NSAppleEventDescriptor *event = [NSAppleEventDescriptor
        appleEventWithEventClass:'Ghst' eventID:'NWin'
        targetDescriptor:[NSAppleEventDescriptor descriptorWithProcessIdentifier:pid]
        returnID:kAutoGenerateReturnID transactionID:kAnyTransactionID];
    [event setParamDescriptor:config forKeyword:'GNwS'];
    return event;
}

bool task_ghostty_dictionary_supports_window(NSXMLDocument *dictionary) {
    // Detect the consumed contract, not an application-version allowlist.
    for (NSString *query in @[
        @"//command[@code='GhstNWin']/parameter[@code='GNwS']",
        @"//record-type[@code='GScf']/property[@code='GScC'][@type='text']",
        @"//record-type[@code='GScf']/property[@code='GScW'][@type='boolean']"
    ]) {
        if ([[dictionary nodesForXPath:query error:nil] count] != 1) return false;
    }
    return true;
}

static void task_ghostty_completed(BotGhosttyLaunch *state, NSRunningApplication *app, NSError *error, NSSet *previous, NSString *text) {
    // Completion always updates the shared owner, even after the
    // caller cancelled before receiving a PID. Script cancellation
    // suppresses creation, not ownership of an already launched GUI.
    BOOL owned = bot_terminal_instance_complete((__bridge void *)state.instance, app, previous);
    @synchronized(state) {
        if (!owned) {
            state.status = -3; return; // Never send the create event to a user's existing instance.
        }
        state.pid = app.processIdentifier;
        if (state.cancelled) return;
        if (!app || error || app.terminated || app.processIdentifier <= 0) {
            state.error = error.code;
            state.status = -3;
            return;
        }
    }
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
        @autoreleasepool {
            @synchronized(state) { if (state.cancelled) return; }
            NSError *sendError = nil;
            NSAppleEventDescriptor *event = task_ghostty_create_event(app.processIdentifier, text);
            // TCC may ask for Automation consent. Never suppress it or
            // retry via documents/CLI after denial or an unknown reply.
            NSAppleEventDescriptor *reply = [event sendEventWithOptions:
                NSAppleEventSendWaitForReply | NSAppleEventSendCanInteract
                timeout:300 error:&sendError];
            long code = sendError ? sendError.code : [reply paramDescriptorForKeyword:keyErrorNumber].int32Value;
            dispatch_async(dispatch_get_main_queue(), ^{
                @synchronized(state) {
                    if (state.cancelled) return;
                    state.error = code;
                    state.status = (code == errAEEventNotPermitted || code == errAEPrivilegeError) ? -2 :
                        ((!reply || code) ? -3 : 1);
                    // Do not activate before the creation reply, on
                    // denial, or after a cancelled opening.
                    if (state.status == 1 && !task_ghostty_show(app)) state.status = -4;
                }
            });
        }
    });
}

void *task_ghostty_open(const char *command) {
    @autoreleasepool {
        BotGhosttyLaunch *state = [BotGhosttyLaunch new];
        state.instance = (__bridge_transfer id)bot_terminal_instance_pending("com.mitchellh.ghostty");
        NSString *text = [NSString stringWithUTF8String:command];
        dispatch_async(dispatch_get_main_queue(), ^{
            @synchronized(state) {
                if (state.cancelled) {
                    bot_terminal_instance_not_submitted((__bridge void *)state.instance);
                    return;
                }
            }
            NSURL *url = [NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:@"com.mitchellh.ghostty"];
            NSBundle *bundle = url ? [NSBundle bundleWithURL:url] : nil;
            NSString *definition = [bundle objectForInfoDictionaryKey:@"OSAScriptingDefinition"];
            NSURL *dictionaryURL = definition ? [bundle.resourceURL URLByAppendingPathComponent:definition] : nil;
            NSXMLDocument *dictionary = dictionaryURL ? [[NSXMLDocument alloc]
                initWithContentsOfURL:dictionaryURL options:NSXMLNodeLoadExternalEntitiesNever error:nil] : nil;
            if (!task_ghostty_dictionary_supports_window(dictionary)) {
                bot_terminal_instance_not_submitted((__bridge void *)state.instance);
                @synchronized(state) { state.status = -1; }
                return;
            }
            NSMutableSet *previous = [NSMutableSet set];
            for (NSRunningApplication *running in [NSRunningApplication runningApplicationsWithBundleIdentifier:@"com.mitchellh.ghostty"])
                [previous addObject:@(running.processIdentifier)];
            [NSWorkspace.sharedWorkspace openApplicationAtURL:url
                configuration:task_ghostty_configuration()
                completionHandler:^(NSRunningApplication *app, NSError *error) {
                task_ghostty_completed(state, app, error, previous, text);
            }];
        });
        return (__bridge_retained void *)state;
    }
}

void *task_ghostty_open_instance(void *handle) {
    BotGhosttyLaunch *state = (__bridge BotGhosttyLaunch *)handle;
    return (__bridge_retained void *)state.instance;
}

int task_ghostty_open_poll(void *handle, int *pid, long *error) {
    BotGhosttyLaunch *state = (__bridge BotGhosttyLaunch *)handle;
    @synchronized(state) {
        *pid = state.pid;
        *error = state.error;
        return state.status;
    }
}

void task_ghostty_open_release(void *handle) {
    BotGhosttyLaunch *state = (__bridge_transfer BotGhosttyLaunch *)handle;
    @synchronized(state) { state.cancelled = YES; }
}
