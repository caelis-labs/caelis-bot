// Compile the actual bridge with an acknowledgement stub. No fixture code
// registers for system power, requests sleep or acknowledges a real OS event.
#define IOAllowPowerChange fixture_allow_power_change
#include "../bind_darwin_cgo.m"
#include <stdio.h>

static pthread_mutex_t fixture_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t fixture_condition = PTHREAD_COND_INITIALIZER;
static bool stop_entered, stop_may_finish, stopped;
static int acknowledgements, wakes, failure;

void caelisManagedLeasePower(uintptr_t callback_id, int waking) {
    pthread_mutex_lock(&fixture_mutex);
    if (callback_id != 7) failure = 1;
    if (waking) {
        if (!stopped) failure = 2;
        wakes++;
    } else {
        stop_entered = true;
        pthread_cond_signal(&fixture_condition);
        while (!stop_may_finish) pthread_cond_wait(&fixture_condition, &fixture_mutex);
        stopped = true;
    }
    pthread_mutex_unlock(&fixture_mutex);
}

IOReturn fixture_allow_power_change(io_connect_t root, intptr_t notification) {
    pthread_mutex_lock(&fixture_mutex);
    if (!stopped || root != 0 || notification != 42) failure = 3;
    acknowledgements++;
    pthread_mutex_unlock(&fixture_mutex);
    return kIOReturnSuccess;
}

static void *fixture_prepare(void *state) {
    power_callback(state, 0, kIOMessageSystemWillSleep, (void *)42);
    return NULL;
}

int main(void) {
    CaelisLeasePower state = {.callback = 7};
    pthread_t callback;
    if (pthread_create(&callback, NULL, fixture_prepare, &state)) return 10;
    pthread_mutex_lock(&fixture_mutex);
    while (!stop_entered) pthread_cond_wait(&fixture_condition, &fixture_mutex);
    if (acknowledgements != 0) failure = 4;
    stop_may_finish = true;
    pthread_cond_signal(&fixture_condition);
    pthread_mutex_unlock(&fixture_mutex);
    pthread_join(callback, NULL);
    power_callback(&state, 0, kIOMessageSystemHasPoweredOn, NULL);
    if (acknowledgements != 1 || wakes != 1 || !stopped) failure = 5;
    if (failure) fprintf(stderr, "native acknowledgement order failure %d\n", failure);
    return failure;
}
