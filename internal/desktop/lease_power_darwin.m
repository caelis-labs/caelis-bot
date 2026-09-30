//go:build darwin && cgo
#import <Foundation/Foundation.h>
#import <IOKit/IOKitLib.h>
#import <IOKit/IOMessage.h>
#import <IOKit/pwr_mgt/IOPMLib.h>
#import "lease_power_darwin.h"
extern void botManagedLeasePower(uintptr_t callback_id, int waking);
typedef struct { io_connect_t root; IONotificationPortRef port; io_object_t notifier; uintptr_t callback; } BotLeasePower;
static void leasePowerCallback(void *ref, io_service_t service, natural_t message, void *argument) {
 BotLeasePower *state=ref;
 if(message==kIOMessageCanSystemSleep)IOAllowPowerChange(state->root,(long)argument);
 else if(message==kIOMessageSystemWillSleep){
  // The native owner revokes and fences its owned processes before acknowledging.
  botManagedLeasePower(state->callback,0);
  IOAllowPowerChange(state->root,(long)argument);
 } else if(message==kIOMessageSystemHasPoweredOn)botManagedLeasePower(state->callback,1);
}
uintptr_t bot_lease_power_register(uintptr_t callback_id){
 __block BotLeasePower *state=NULL;
 void (^bind)(void)=^{
  state=calloc(1,sizeof(BotLeasePower));if(!state)return;
  state->callback=callback_id;
  state->root=IORegisterForSystemPower(state,&state->port,leasePowerCallback,&state->notifier);
  if(!state->root||!state->port){if(state->port)IONotificationPortDestroy(state->port);free(state);state=NULL;return;}
  CFRunLoopAddSource(CFRunLoopGetMain(),IONotificationPortGetRunLoopSource(state->port),kCFRunLoopCommonModes);
 };
 if(NSThread.isMainThread)bind();else dispatch_sync(dispatch_get_main_queue(),bind);
 return (uintptr_t)state;
}
void bot_lease_power_unregister(uintptr_t handle){
 BotLeasePower *state=(BotLeasePower*)handle;if(!state)return;
 dispatch_async(dispatch_get_main_queue(),^{
  IODeregisterForSystemPower(&state->notifier);
  CFRunLoopRemoveSource(CFRunLoopGetMain(),IONotificationPortGetRunLoopSource(state->port),kCFRunLoopCommonModes);
  IOServiceClose(state->root);IONotificationPortDestroy(state->port);free(state);
 });
}
