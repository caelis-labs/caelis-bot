//go:build darwin && cgo

#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/IOMessage.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdlib.h>
#include "bind_darwin_cgo.h"

extern void caelisManagedLeasePower(uintptr_t callback_id, int waking);

typedef struct {
    pthread_t thread;
    pthread_mutex_t mutex;
    pthread_cond_t ready_condition;
    bool ready;
    bool registered;
    bool closing;
    CFRunLoopRef loop;
    io_connect_t root;
    IONotificationPortRef port;
    io_object_t notifier;
    uintptr_t callback;
} CaelisLeasePower;

static void power_callback(void *ref, io_service_t service, natural_t message, void *argument) {
    CaelisLeasePower *state = ref;
    if (message == kIOMessageCanSystemSleep) {
        // Permission to attempt idle sleep is distinct from its preparation.
        IOAllowPowerChange(state->root, (intptr_t)argument);
    } else if (message == kIOMessageSystemWillSleep) {
        // The synchronous native owner freezes/kills its exact processes and
        // confirms that barrier before the OS receives acknowledgement.
        caelisManagedLeasePower(state->callback, 0);
        IOAllowPowerChange(state->root, (intptr_t)argument);
    } else if (message == kIOMessageSystemHasPoweredOn) {
        caelisManagedLeasePower(state->callback, 1);
    }
}

static void *power_thread(void *argument) {
    CaelisLeasePower *state = argument;
    CFRunLoopRef loop = CFRunLoopGetCurrent();
    state->root = IORegisterForSystemPower(state, &state->port, power_callback, &state->notifier);
    CFRunLoopSourceRef source = state->port ? IONotificationPortGetRunLoopSource(state->port) : NULL;
    bool registered = state->root && state->port && source;
    if (registered) CFRunLoopAddSource(loop, source, kCFRunLoopDefaultMode);
    pthread_mutex_lock(&state->mutex);
    state->registered = registered;
    if (registered) state->loop = (CFRunLoopRef)CFRetain(loop);
    state->ready = true;
    pthread_cond_signal(&state->ready_condition);
    pthread_mutex_unlock(&state->mutex);
    if (registered) CFRunLoopRun();

    pthread_mutex_lock(&state->mutex);
    bool unexpected_exit = registered && !state->closing;
    state->loop = NULL;
    pthread_mutex_unlock(&state->mutex);
    if (unexpected_exit) caelisManagedLeasePower(state->callback, 0);
    if (state->notifier) IODeregisterForSystemPower(&state->notifier);
    if (registered) CFRunLoopRemoveSource(loop, source, kCFRunLoopDefaultMode);
    if (state->root) IOServiceClose(state->root);
    if (state->port) IONotificationPortDestroy(state->port);
    if (registered) CFRelease(loop);
    return NULL;
}

static void destroy_state(CaelisLeasePower *state) {
    pthread_cond_destroy(&state->ready_condition);
    pthread_mutex_destroy(&state->mutex);
    free(state);
}

uintptr_t caelis_lease_power_register(uintptr_t callback_id) {
    CaelisLeasePower *state = calloc(1, sizeof(*state));
    if (!state) return 0;
    state->callback = callback_id;
    if (pthread_mutex_init(&state->mutex, NULL)) { free(state); return 0; }
    if (pthread_cond_init(&state->ready_condition, NULL)) { pthread_mutex_destroy(&state->mutex); free(state); return 0; }
    if (pthread_create(&state->thread, NULL, power_thread, state)) { destroy_state(state); return 0; }
    pthread_mutex_lock(&state->mutex);
    while (!state->ready) pthread_cond_wait(&state->ready_condition, &state->mutex);
    bool registered = state->registered;
    pthread_mutex_unlock(&state->mutex);
    if (!registered) { pthread_join(state->thread, NULL); destroy_state(state); return 0; }
    return (uintptr_t)state;
}

void caelis_lease_power_unregister(uintptr_t handle) {
    CaelisLeasePower *state = (CaelisLeasePower *)handle;
    if (!state) return;
    pthread_mutex_lock(&state->mutex);
    state->closing = true;
    CFRunLoopRef loop = state->loop ? (CFRunLoopRef)CFRetain(state->loop) : NULL;
    pthread_mutex_unlock(&state->mutex);
    if (loop) {
        // Queueing a stop block also covers release immediately after bind,
        // before CFRunLoopRun has entered; an early CFRunLoopStop alone is racy.
        CFRunLoopPerformBlock(loop, kCFRunLoopDefaultMode, ^{ CFRunLoopStop(loop); });
        CFRunLoopWakeUp(loop);
        CFRelease(loop);
    }
    pthread_join(state->thread, NULL);
    destroy_state(state);
}
