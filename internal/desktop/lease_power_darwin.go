//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework IOKit
#include "lease_power_darwin.h"
*/
import "C"
import (
	"errors"
	"sync"
)

var managedPowerRegistry = struct {
	sync.Mutex
	next      uintptr
	callbacks map[uintptr]func(bool)
}{callbacks: map[uintptr]func(bool){}}

// BindManagedLeasePower is opt-in for a leased managed runtime. Default local
// execution never registers this additional system-power owner.
func BindManagedLeasePower(suspend func(), wake func()) (func(), error) {
	managedPowerRegistry.Lock()
	managedPowerRegistry.next++
	id := managedPowerRegistry.next
	managedPowerRegistry.callbacks[id] = func(waking bool) {
		if waking {
			wake()
		} else {
			suspend()
		}
	}
	managedPowerRegistry.Unlock()
	handle := C.bot_lease_power_register(C.uintptr_t(id))
	if handle == 0 {
		managedPowerRegistry.Lock()
		delete(managedPowerRegistry.callbacks, id)
		managedPowerRegistry.Unlock()
		return nil, errors.New("native system-power lease fencing unavailable")
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			managedPowerRegistry.Lock()
			delete(managedPowerRegistry.callbacks, id)
			managedPowerRegistry.Unlock()
			C.bot_lease_power_unregister(handle)
		})
	}, nil
}

//export botManagedLeasePower
func botManagedLeasePower(id C.uintptr_t, waking C.int) {
	managedPowerRegistry.Lock()
	callback := managedPowerRegistry.callbacks[uintptr(id)]
	managedPowerRegistry.Unlock()
	if callback != nil {
		callback(waking != 0)
	}
}
