//go:build darwin && cgo
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#import "instance_darwin.h"
#include <libproc.h>
#include <signal.h>
#include <errno.h>
#include <sys/proc.h>
#ifndef BOT_INSTANCE_APPLICATION_URL
#define BOT_INSTANCE_APPLICATION_URL(bundle) [NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:bundle]
#endif

// Ownership comes from a fresh LaunchServices result, never a title, TTY,
// terminal session, or the number/order of its windows. All state is main-only.
@interface BotTerminalInstance : NSObject
@property NSRunningApplication *app;
@property NSString *bundle;
@property uint64_t birth;
@property int ready;
@property int closeResult;
@property int documentResult;
@property NSUInteger documentSequence;
@end
@implementation BotTerminalInstance
@end

static void instance_main(dispatch_block_t block) {
    if (NSThread.isMainThread) block(); else dispatch_sync(dispatch_get_main_queue(), block);
}
static uint64_t instance_birth(int pid) {
    struct proc_bsdinfo info = {0};
    if (pid <= 0 || proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) return 0;
    return info.pbi_start_tvsec * 1000000ULL + info.pbi_start_tvusec;
}
int bot_terminal_client_state(int pid, uint64_t expected_birth, uint64_t *birth) {
    *birth = 0;
    if (pid <= 0) return -1;
    struct proc_bsdinfo info = {0};
    if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, sizeof(info)) != sizeof(info)) {
        return kill(pid, 0) == -1 && errno == ESRCH ? 0 : -1;
    }
    *birth = info.pbi_start_tvsec * 1000000ULL + info.pbi_start_tvusec;
    if (info.pbi_status == SZOMB || (expected_birth && *birth != expected_birth)) return 0;
    return *birth ? 1 : -1;
}
static BOOL instance_accept(BotTerminalInstance *owner, NSRunningApplication *app, NSSet *previous) {
    if (!app || app.terminated || app.processIdentifier <= 0 ||
        ![app.bundleIdentifier isEqual:owner.bundle] || [previous containsObject:@(app.processIdentifier)]) return NO;
    owner.birth = instance_birth(app.processIdentifier);
    if (!owner.birth) return NO;
    owner.app = app;
    return YES;
}
void *bot_terminal_instance_pending(const char *bundle) {
    BotTerminalInstance *owner = [BotTerminalInstance new];
    owner.bundle = [NSString stringWithUTF8String:bundle];
    return (__bridge_retained void *)owner;
}
void bot_terminal_instance_not_submitted(void *handle) {
    instance_main(^{ ((__bridge BotTerminalInstance *)handle).ready = -2; });
}
BOOL bot_terminal_instance_complete(void *handle, NSRunningApplication *app, NSSet *previous) {
    __block BOOL accepted;
    instance_main(^{
        BotTerminalInstance *owner = (__bridge BotTerminalInstance *)handle;
        accepted = instance_accept(owner, app, previous);
        owner.ready = accepted ? 1 : -1;
    });
    return accepted;
}
static NSWorkspaceOpenConfiguration *instance_configuration(void) {
    NSWorkspaceOpenConfiguration *config = [NSWorkspaceOpenConfiguration configuration];
    config.createsNewApplicationInstance = YES;
    config.allowsRunningApplicationSubstitution = NO;
    config.activates = YES;
    config.addsToRecentItems = NO;
    return config;
}
void *bot_terminal_instance_open(const char *bundle, const char *script) {
    @autoreleasepool {
        BotTerminalInstance *owner = [BotTerminalInstance new];
        owner.bundle = [NSString stringWithUTF8String:bundle];
        NSString *document = [NSString stringWithUTF8String:script];
        dispatch_async(dispatch_get_main_queue(), ^{
            NSURL *url = BOT_INSTANCE_APPLICATION_URL(owner.bundle);
            if (!url) { owner.ready = -2; return; } // No native request was submitted.
            NSMutableSet *previous = [NSMutableSet set];
            for (NSRunningApplication *app in [NSRunningApplication runningApplicationsWithBundleIdentifier:owner.bundle])
                [previous addObject:@(app.processIdentifier)];
            void (^completed)(NSRunningApplication *, NSError *) = ^(NSRunningApplication *app, NSError *error) {
                    instance_main(^{
                        // An error may accompany a launched app. Retain that
                        // identity so a later click cannot create duplicates.
                        if (instance_accept(owner, app, previous)) {
                            owner.ready = 1;
                            bot_terminal_instance_open_document((__bridge void *)owner, document.UTF8String);
                        }
                        else { owner.ready = -1; NSLog(@"Task terminal instance launch not owned (code=%ld)",(long)error.code); }
                    });
                };
            // Obtain the new process first, then deliver exactly one standard
            // document event with an observable reply. LaunchServices completion
            // alone says nothing about the user's document confirmation.
            [NSWorkspace.sharedWorkspace openApplicationAtURL:url
                configuration:instance_configuration() completionHandler:completed];
        });
        return (__bridge_retained void *)owner;
    }
}
void *bot_terminal_instance_adopt(int pid, const char *bundle) {
    @autoreleasepool {
        BotTerminalInstance *owner = [BotTerminalInstance new];
        owner.bundle = [NSString stringWithUTF8String:bundle];
        instance_main(^{
            owner.ready = instance_accept(owner, [NSRunningApplication runningApplicationWithProcessIdentifier:pid], [NSSet set]) ? 1 : -1;
        });
        return (__bridge_retained void *)owner;
    }
}
int bot_terminal_instance_ready(void *handle) {
    if (!handle) return -1;
    __block int ready; instance_main(^{ ready = ((__bridge BotTerminalInstance *)handle).ready; }); return ready;
}
int bot_terminal_instance_open_document(void *handle, const char *script) {
    int state,pid; uint64_t birth;
    if(bot_terminal_instance_observe(handle,&state,&pid,&birth)!=1 || state==1) return -1;
    NSURL *document=[NSURL fileURLWithPath:[NSString stringWithUTF8String:script]];
    instance_main(^{
        BotTerminalInstance *owner=(__bridge BotTerminalInstance *)handle;
        NSUInteger sequence=++owner.documentSequence;
        owner.documentResult=1;
        dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED,0),^{ @autoreleasepool {
            int result=-1;
            if(instance_birth(pid)==birth) {
                NSAppleEventDescriptor *event=[NSAppleEventDescriptor appleEventWithEventClass:kCoreEventClass eventID:kAEOpenDocuments targetDescriptor:[NSAppleEventDescriptor descriptorWithProcessIdentifier:pid] returnID:kAutoGenerateReturnID transactionID:kAnyTransactionID];
                NSAppleEventDescriptor *files=NSAppleEventDescriptor.listDescriptor;
                [files insertDescriptor:[NSAppleEventDescriptor descriptorWithFileURL:document] atIndex:1];
                [event setParamDescriptor:files forKeyword:keyDirectObject];
                NSError *error=nil;
                NSAppleEventDescriptor *reply=[event sendEventWithOptions:NSAppleEventSendWaitForReply|NSAppleEventSendCanInteract|NSAppleEventSendCanSwitchLayer timeout:300 error:&error];
                NSInteger code=error ? error.code : [reply paramDescriptorForKeyword:keyErrorNumber].int32Value;
                result=2;
                if(code==userCanceledErr)result=3;
                else if(code==errAETimeout)result=4;
                else if(code==errAEEventNotPermitted || code==errAEPrivilegeError)result=-2;
                else if(code==errAEEventNotHandled)result=-3;
                else if(code || !reply)result=-1;
            }
            dispatch_async(dispatch_get_main_queue(),^{
                if(owner.documentSequence==sequence)owner.documentResult=result;
            });
        }});
    });
    return 1;
}
int bot_terminal_instance_document_result(void *handle) {
    if(!handle)return -1;
    __block int result;instance_main(^{result=((__bridge BotTerminalInstance *)handle).documentResult;});return result;
}
int bot_terminal_instance_observe(void *handle, int *state, int *pid, uint64_t *birth) {
    *state = 0; *pid = 0; *birth = 0;
    if (!handle) return -1;
    __block int result = 1;
    instance_main(^{
        BotTerminalInstance *owner = (__bridge BotTerminalInstance *)handle;
        NSRunningApplication *app = owner.app;
        if (owner.ready == 0) { result = 0; return; }
        if (owner.ready == -2) { *state = 1; return; } // Proven no launch, safe to retry.
        if (!app) { result = -1; return; } // No authority over a reused/unknown application.
        if (app.terminated) { *state = 1; return; }
        uint64_t live = instance_birth(app.processIdentifier);
        if (!live) {
            if (kill(app.processIdentifier,0)==-1 && errno==ESRCH) *state=1;
            else result=0; // A transient/denied read is not proof of exit; reconcile within the caller deadline.
            return;
        }
        if (live != owner.birth) { *state = 1; return; }
        *pid = app.processIdentifier; *birth = owner.birth;
        BOOL foreground=NSWorkspace.sharedWorkspace.frontmostApplication.processIdentifier==app.processIdentifier;
        *state = app.hidden ? 2 : (foreground ? 4 : 3);
    });
    return result;
}
int bot_terminal_instance_apply(void *handle, int command) {
    int state, pid; uint64_t birth;
    if (bot_terminal_instance_observe(handle, &state, &pid, &birth) != 1 || state == 1) return -1;
    __block BOOL submitted = NO;
    __block int failure = -1;
    instance_main(^{
        BotTerminalInstance *owner = (__bridge BotTerminalInstance *)handle;
        NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:owner.app.processIdentifier];
        if (!app || app.terminated || instance_birth(app.processIdentifier) != owner.birth) return;
        if (command == 1) { submitted=YES; [app terminate]; }
        else if (command == 3) { submitted=YES; if (!app.hidden) [app hide]; }
        else if (command == 2) {
            // Activation itself unhides and raises the group. An explicit unhide
            // immediately followed by activation races two system transactions.
            // Submit one operation, then observe; never issue a timed retry.
            NSAppleEventDescriptor *event=[NSAppleEventDescriptor appleEventWithEventClass:kAEMiscStandards eventID:kAEActivate targetDescriptor:[NSAppleEventDescriptor descriptorWithProcessIdentifier:app.processIdentifier] returnID:kAutoGenerateReturnID transactionID:kAnyTransactionID];
            NSError *error=nil;
            [event sendEventWithOptions:NSAppleEventSendNoReply timeout:1 error:&error];
            submitted = error == nil;
            if (error.code == errAEEventNotPermitted || error.code == errAEPrivilegeError) failure = -2;
            if (error) NSLog(@"Task terminal instance activation error=%ld",(long)error.code);
        }
    });
    // NSRunningApplication may return NO even when its queued request takes
    // effect on the next run-loop turn. Only observed state confirms completion;
    // a return value never triggers a second AX/script/activation transaction.
    return submitted ? 1 : failure;
}
void bot_terminal_instance_release(void *handle) {
    if (!handle) return;
    // Dropping the observation handle never quits a terminal or its Worker.
    instance_main(^{ CFRelease(handle); });
}
int bot_terminal_instance_request_close(void *handle) {
    int state,pid; uint64_t birth;
    if(bot_terminal_instance_observe(handle,&state,&pid,&birth)!=1 || state==1) return -1;
    instance_main(^{
        BotTerminalInstance *owner=(__bridge BotTerminalInstance *)handle;
        if(owner.closeResult==1) return; // One pending request, never another prompt.
        owner.closeResult=1;
        dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED,0),^{ @autoreleasepool {
            if(instance_birth(pid)!=birth) {
                dispatch_async(dispatch_get_main_queue(),^{ owner.closeResult=2; });return;
            }
            // Standard Quit with a reply distinguishes user cancellation from
            // failure. Waiting is off the main queue so Bot remains responsive.
            NSAppleEventDescriptor *event=[NSAppleEventDescriptor appleEventWithEventClass:kCoreEventClass eventID:kAEQuitApplication targetDescriptor:[NSAppleEventDescriptor descriptorWithProcessIdentifier:pid] returnID:kAutoGenerateReturnID transactionID:kAnyTransactionID];
            NSError *error=nil;
            NSAppleEventDescriptor *reply=[event sendEventWithOptions:NSAppleEventSendWaitForReply|NSAppleEventSendCanInteract|NSAppleEventSendCanSwitchLayer timeout:300 error:&error];
            NSInteger code=error ? error.code : [reply paramDescriptorForKeyword:keyErrorNumber].int32Value;
            int result=2;
            if(code==userCanceledErr) result=3;
            else if(code==errAETimeout) result=4;
            else if(code==errAEEventNotPermitted || code==errAEPrivilegeError) result=-2;
            else if(code || !reply) result=-1;
            dispatch_async(dispatch_get_main_queue(),^{ owner.closeResult=result; });
        }});
    });
    return 1;
}
int bot_terminal_instance_close_result(void *handle) {
    if(!handle) return -1;
    __block int result;instance_main(^{ result=((__bridge BotTerminalInstance *)handle).closeResult; });return result;
}
uint64_t bot_terminal_input_epoch(void) {
    return (uint64_t)CGEventSourceCounterForEventType(kCGEventSourceStateCombinedSessionState,kCGEventLeftMouseDown)
        + CGEventSourceCounterForEventType(kCGEventSourceStateCombinedSessionState,kCGEventRightMouseDown)
        + CGEventSourceCounterForEventType(kCGEventSourceStateCombinedSessionState,kCGEventOtherMouseDown)
        + CGEventSourceCounterForEventType(kCGEventSourceStateCombinedSessionState,kCGEventKeyDown);
}
