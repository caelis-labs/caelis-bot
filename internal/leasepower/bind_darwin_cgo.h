#ifndef CAELIS_LEASE_POWER_DARWIN_H
#define CAELIS_LEASE_POWER_DARWIN_H
#include <stdint.h>
uintptr_t caelis_lease_power_register(uintptr_t callback_id);
void caelis_lease_power_unregister(uintptr_t handle);
#endif
