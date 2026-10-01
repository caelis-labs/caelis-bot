//go:build darwin && cgo

package leasepower

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework CoreFoundation -framework IOKit
#include "bind_darwin_cgo.h"
*/
import "C"

import (
	"context"
	"sync"
)

func markCloseOnExec(int) {}

var darwinPowerRegistry = struct {
	sync.Mutex
	next     uintptr
	bindings map[uintptr]*darwinBinding
}{bindings: map[uintptr]*darwinBinding{}}

type darwinBinding struct {
	mu            sync.Mutex
	closed        bool
	suspend, wake func()
}

func (b *darwinBinding) deliver(waking bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if waking {
		b.wake()
	} else {
		b.suspend()
	}
}

func (b *darwinBinding) fenceAndClose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.suspend()
}

// BindWithOptions registers a dedicated IOKit power run loop. No application,
// main-thread dispatch, AppKit or Wails loop is needed by a headless owner.
func BindWithOptions(ctx context.Context, suspend, wake func(), opts Options) (func(), error) {
	return bindDarwin(ctx, suspend, wake, opts, func(id uintptr) uintptr {
		return uintptr(C.caelis_lease_power_register(C.uintptr_t(id)))
	}, func(handle uintptr) { C.caelis_lease_power_unregister(C.uintptr_t(handle)) })
}

func bindDarwin(ctx context.Context, suspend, wake func(), opts Options, register func(uintptr) uintptr, unregister func(uintptr)) (func(), error) {
	if _, err := checkedOptions(suspend, wake, opts); err != nil {
		if suspend != nil {
			suspend()
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		suspend()
		return nil, unavailable("Darwin power binding context ended", err)
	}
	b := &darwinBinding{suspend: suspend, wake: wake}
	darwinPowerRegistry.Lock()
	darwinPowerRegistry.next++
	id := darwinPowerRegistry.next
	darwinPowerRegistry.bindings[id] = b
	darwinPowerRegistry.Unlock()
	remove := func() {
		darwinPowerRegistry.Lock()
		delete(darwinPowerRegistry.bindings, id)
		darwinPowerRegistry.Unlock()
	}
	handle := register(id)
	if handle == 0 {
		b.fenceAndClose()
		remove()
		return nil, unavailable("IOKit system-power registration failed", nil)
	}
	if err := ctx.Err(); err != nil {
		b.fenceAndClose()
		unregister(handle)
		remove()
		return nil, unavailable("Darwin power binding context ended during registration", err)
	}
	done := make(chan struct{})
	var once sync.Once
	release := func() {
		once.Do(func() {
			b.fenceAndClose()
			// Keep the registry entry alive until the native thread has stopped:
			// no in-flight callback can outlive release or reference retired state.
			unregister(handle)
			remove()
			close(done)
		})
		<-done
	}
	go func() {
		select {
		case <-ctx.Done():
			release()
		case <-done:
		}
	}()
	return release, nil
}

func deliverDarwinPower(id uintptr, waking bool) {
	darwinPowerRegistry.Lock()
	b := darwinPowerRegistry.bindings[id]
	darwinPowerRegistry.Unlock()
	if b != nil {
		b.deliver(waking)
	}
}

//export caelisManagedLeasePower
func caelisManagedLeasePower(id C.uintptr_t, waking C.int) {
	deliverDarwinPower(uintptr(id), waking != 0)
}
