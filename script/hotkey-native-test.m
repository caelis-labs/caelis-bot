// Direct callback fixtures below do not synthesize physical keyboard input.
#import "../internal/desktop/hotkey_darwin.m"
#include <stdio.h>

@interface TestHotkeys : BotHotkeys
@property BOOL physicalDown;
@end
@implementation TestHotkeys
- (BOOL)keyIsDown:(BotHotkeySlot *)slot { return self.physicalDown; }
@end
static void require(BOOL value,NSString *message) {
    if(!value){fprintf(stderr,"FAIL: %s\n",message.UTF8String);exit(1);}
}
static OSStatus event(TestHotkeys *keys,NSUInteger slot,BOOL down,UInt32 revision) {
    EventRef value=NULL;
    CreateEvent(NULL,kEventClassKeyboard,down?kEventHotKeyPressed:kEventHotKeyReleased,0,kEventAttributeNone,&value);
    EventHotKeyID id={'Cael',((UInt32)slot<<28)|revision};
    SetEventParameter(value,kEventParamDirectObject,typeEventHotKeyID,sizeof(id),&id);
    OSStatus result=[keys receive:value];ReleaseEvent(value);return result;
}
int main(int argc,char **argv) {@autoreleasepool {
    NSString *mode=argc>1?@(argv[1]):@"callbacks";
    if([mode isEqual:@"owner"] || [mode isEqual:@"retry"]){
        BotHotkeys *keys=[BotHotkeys new];
        OSStatus status=[keys configureKey:@"F12" flags:11 enabled:YES slot:2];
        printf("%d\n",(int)status);fflush(stdout);
        if([mode isEqual:@"retry"] && status==eventHotKeyExistsErr) {
            for(int i=0;i<100 && status==eventHotKeyExistsErr;i++) {
                usleep(50000);
                status=[keys configureKey:@"F12" flags:11 enabled:YES slot:2];
            }
            printf("%d\n",(int)status);fflush(stdout);
        }
        getchar();[keys invalidate];return 0;
    }
    if([mode isEqual:@"probe-f1"]){
        BotHotkeys *keys=[BotHotkeys new];
        OSStatus status=[keys configureKey:@"F1" flags:0 enabled:YES slot:2];
        printf("%d\n",(int)status);fflush(stdout);
        getchar();[keys invalidate];return 0;
    }
    TestHotkeys *keys=[TestHotkeys new];
    __block int presses=0,releases=0;
    keys.action=^(NSUInteger slot,BOOL down){if(down)presses++;else releases++;};
    require([keys configureKey:@"F12" flags:11 enabled:YES slot:2]==noErr,@"initial registration");
    UInt32 rev=keys.slots[2].revision;presses=releases=0;
    keys.physicalDown=YES;
    for(int i=0;i<20;i++){event(keys,2,YES,rev);[keys reconcileReleases];}
    require(presses==1&&releases==0,@"long hold/repeats must emit one press");
    keys.physicalDown=NO;[keys reconcileReleases];
    require(releases==1&&!keys.slots[2].down,@"missing key-up must clear the latch");
    keys.physicalDown=YES;event(keys,2,YES,rev);
    require(presses==2,@"first new press after missing release must work");
    event(keys,2,NO,rev);event(keys,2,NO,rev);
    require(releases==2,@"duplicate release");
    require([keys configureKey:@"F11" flags:11 enabled:YES slot:2]==noErr,@"remap");
    require(event(keys,2,YES,rev)==eventNotHandledErr&&presses==2,@"stale generation event");
    rev=keys.slots[2].revision;
    require([keys configureKey:@"F11" flags:11 enabled:NO slot:2]==noErr,@"disable");
    require(event(keys,2,YES,rev)==eventNotHandledErr&&presses==2,@"disabled event");
    [keys invalidate];require(!keys.releaseTimer&&!keys.handler,@"shutdown cleanup");
    puts("PASS callback_fixture: held repeat, missing release, next press, remap revision, disabled, shutdown");
    return 0;
}}
