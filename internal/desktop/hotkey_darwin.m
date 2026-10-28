//go:build darwin && cgo

#import "hotkey_darwin.h"
#import <os/log.h>

@interface BotHotkeySlot : NSObject
@property EventHotKeyRef registration;
@property NSString *key;
@property int flags;
@property CGKeyCode code;
@property UInt32 revision;
@property BOOL down;
@property NSTimeInterval lastRecoveryLog;
@end
@implementation BotHotkeySlot
@end

@interface BotHotkeys ()
@property NSArray<BotHotkeySlot *> *slots;
@property EventHandlerRef handler;
@property NSTimer *releaseTimer;
- (OSStatus)receive:(EventRef)event;
- (BOOL)keyIsDown:(BotHotkeySlot *)slot;
@end

static OSStatus hotkeyEvent(EventHandlerCallRef next, EventRef event, void *context) {
    return [(__bridge BotHotkeys *)context receive:event];
}

@implementation BotHotkeys
- (instancetype)init {
    if ((self = [super init])) self.slots = @[[BotHotkeySlot new], [BotHotkeySlot new], [BotHotkeySlot new], [BotHotkeySlot new]];
    return self;
}
- (BOOL)keyIsDown:(BotHotkeySlot *)slot {
    return CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState, slot.code);
}
- (void)reconcileReleases {
    BOOL held = NO;
    for (NSUInteger i = 0; i < self.slots.count; i++) {
        BotHotkeySlot *slot = self.slots[i];
        if (!slot.down) continue;
        if ([self keyIsDown:slot]) { held = YES; continue; }
        slot.down = NO;
        if (self.action) self.action(i, NO);
        NSTimeInterval now = NSProcessInfo.processInfo.systemUptime;
        if (now - slot.lastRecoveryLog >= 30) {
            os_log_info(OS_LOG_DEFAULT, "Caelis shortcut recovered missing release: slot=%lu", (unsigned long)i);
            slot.lastRecoveryLog = now;
        }
    }
    if (!held) { [self.releaseTimer invalidate]; self.releaseTimer = nil; }
}
- (OSStatus)receive:(EventRef)event {
    EventHotKeyID id;
    if (GetEventParameter(event,kEventParamDirectObject,typeEventHotKeyID,NULL,sizeof(id),NULL,&id)!=noErr || id.signature!='Cael') return eventNotHandledErr;
    NSUInteger kind = id.id >> 28;
    if (kind >= self.slots.count) return eventNotHandledErr;
    BotHotkeySlot *slot = self.slots[kind];
    if (!slot.registration || (id.id & 0x0fffffff) != slot.revision) return eventNotHandledErr;
    BOOL down = GetEventKind(event) == kEventHotKeyPressed;
    if (slot.down == down) return noErr;
    slot.down = down;
    if (self.action) self.action(kind, down);
    if (down && !self.releaseTimer) {
        __weak BotHotkeys *weak = self;
        self.releaseTimer = [NSTimer timerWithTimeInterval:0.025 repeats:YES block:^(NSTimer *timer) { [weak reconcileReleases]; }];
        [NSRunLoop.mainRunLoop addTimer:self.releaseTimer forMode:NSRunLoopCommonModes];
    }
    return noErr;
}
- (OSStatus)configureKey:(NSString *)key flags:(int)flags enabled:(BOOL)enabled slot:(NSUInteger)kind {
    if (kind >= self.slots.count) return paramErr;
    BotHotkeySlot *slot = self.slots[kind];
    if (!enabled) {
        if (slot.registration) {
            OSStatus status = UnregisterEventHotKey(slot.registration);
            if (status != noErr) return status;
        }
        slot.registration = NULL;
        slot.down = NO;
        if (self.action) self.action(kind, NO);
        [self reconcileReleases];
        return noErr;
    }
    if (slot.registration && [slot.key isEqual:key] && slot.flags == flags) return noErr;
    for (BotHotkeySlot *other in self.slots)
        if (other != slot && other.registration && [other.key isEqual:key] && other.flags == flags) return eventHotKeyExistsErr;
    NSDictionary *codes = @{@"Space":@49,@"KeyA":@0,@"KeyS":@1,@"KeyD":@2,@"KeyF":@3,@"KeyH":@4,@"KeyG":@5,@"KeyZ":@6,@"KeyX":@7,@"KeyC":@8,@"KeyV":@9,@"KeyB":@11,@"KeyQ":@12,@"KeyW":@13,@"KeyE":@14,@"KeyR":@15,@"KeyY":@16,@"KeyT":@17,@"KeyO":@31,@"KeyU":@32,@"KeyI":@34,@"KeyP":@35,@"KeyL":@37,@"KeyJ":@38,@"KeyK":@40,@"KeyN":@45,@"KeyM":@46,@"Digit1":@18,@"Digit2":@19,@"Digit3":@20,@"Digit4":@21,@"Digit6":@22,@"Digit5":@23,@"Digit9":@25,@"Digit7":@26,@"Digit8":@28,@"Digit0":@29,@"F1":@122,@"F2":@120,@"F3":@99,@"F4":@118,@"F5":@96,@"F6":@97,@"F7":@98,@"F8":@100,@"F9":@101,@"F10":@109,@"F11":@103,@"F12":@111};
    NSNumber *code = codes[key];
    if (!code) return paramErr;
    if (!self.handler) {
        EventTypeSpec specs[] = {{kEventClassKeyboard,kEventHotKeyPressed},{kEventClassKeyboard,kEventHotKeyReleased}};
        EventHandlerRef handler = NULL;
        OSStatus status = InstallEventHandler(GetApplicationEventTarget(),hotkeyEvent,2,specs,(__bridge void *)self,&handler);
        if (status != noErr) return status;
        self.handler = handler;
    }
    UInt32 mods = 0;
    if (flags&1) mods|=controlKey;
    if (flags&2) mods|=optionKey;
    if (flags&4) mods|=shiftKey;
    if (flags&8) mods|=cmdKey;
    CFArrayRef symbolic = NULL;
    if (CopySymbolicHotKeys(&symbolic)==noErr && symbolic) {
        BOOL reserved = NO;
        for (NSDictionary *entry in (__bridge NSArray *)symbolic)
            if ([entry[(__bridge NSString *)kHISymbolicHotKeyEnabled] boolValue] && [entry[(__bridge NSString *)kHISymbolicHotKeyCode] unsignedIntValue]==code.unsignedIntValue && ([entry[(__bridge NSString *)kHISymbolicHotKeyModifiers] unsignedIntValue] & (controlKey|optionKey|shiftKey|cmdKey))==mods) { reserved=YES; break; }
        CFRelease(symbolic);
        if (reserved) return eventHotKeyExistsErr;
    }
    UInt32 revision = (slot.revision + 1) & 0x0fffffff;
    EventHotKeyID id = {'Cael', ((UInt32)kind << 28) | revision};
    EventHotKeyRef registration = NULL;
    // Non-exclusive success can be silently suppressed by another application.
    OSStatus status = RegisterEventHotKey(code.unsignedIntValue,mods,id,GetApplicationEventTarget(),kEventHotKeyExclusive,&registration);
    if (status != noErr) return status;
    if (slot.registration) UnregisterEventHotKey(slot.registration);
    if (self.action) self.action(kind, NO);
    slot.registration=registration; slot.revision=revision; slot.key=key; slot.flags=flags; slot.code=code.unsignedShortValue; slot.down=NO;
    return noErr;
}
- (void)invalidate {
    [self.releaseTimer invalidate]; self.releaseTimer=nil;
    for (BotHotkeySlot *slot in self.slots) {
        if (slot.registration) UnregisterEventHotKey(slot.registration);
        slot.registration=NULL; slot.down=NO;
    }
    if (self.handler) { RemoveEventHandler(self.handler); self.handler=NULL; }
    self.action=nil;
}
- (void)dealloc { [self invalidate]; }
@end
