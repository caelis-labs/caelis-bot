//go:build darwin && cgo

package leasepower

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDarwinBindingSerializesFenceBeforeDetachAndNeverRevivesAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var callbackID uintptr
	entered, finish, detached := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var stopped atomic.Bool
	var suspendOnce sync.Once
	var wakeCount atomic.Int32
	release, err := bindDarwin(ctx, func() { suspendOnce.Do(func() { close(entered); <-finish; stopped.Store(true) }) }, func() { wakeCount.Add(1) }, Options{}, func(id uintptr) uintptr { callbackID = id; return 1 }, func(uintptr) {
		if !stopped.Load() {
			t.Error("native detach preceded hard-stop confirmation")
		}
		close(detached)
	})
	if err != nil {
		t.Fatal(err)
	}
	deliveryFinished := make(chan struct{})
	go func() { deliverDarwinPower(callbackID, false); close(deliveryFinished) }()
	waitFor(t, entered)
	cancel()
	select {
	case <-detached:
		t.Fatal("context cancellation bypassed in-flight stop callback")
	default:
	}
	close(finish)
	waitFor(t, deliveryFinished)
	waitFor(t, detached)
	release()
	deliverDarwinPower(callbackID, true)
	if wakeCount.Load() != 0 {
		t.Fatal("retired binding revived ownership")
	}
	darwinPowerRegistry.Lock()
	_, exists := darwinPowerRegistry.bindings[callbackID]
	darwinPowerRegistry.Unlock()
	if exists {
		t.Fatal("retired native callback registration retained")
	}
}

func TestDarwinWakeUsesExistingOwnerCallbackWithoutRebinding(t *testing.T) {
	var callbackID uintptr
	suspends, wakes, registrations, unregistrations := 0, 0, 0, 0
	release, err := bindDarwin(t.Context(), func() { suspends++ }, func() { wakes++ }, Options{}, func(id uintptr) uintptr { callbackID = id; registrations++; return 7 }, func(uintptr) { unregistrations++ })
	if err != nil {
		t.Fatal(err)
	}
	deliverDarwinPower(callbackID, false)
	deliverDarwinPower(callbackID, true)
	if suspends != 1 || wakes != 1 || registrations != 1 || unregistrations != 0 {
		t.Fatal("wake rebound or missed ownership revocation")
	}
	release()
	release()
	if suspends != 2 || unregistrations != 1 {
		t.Fatal("release failed to fence and detach once")
	}
}

func TestDarwinRegistrationFailureAndEndedContextAreUnavailable(t *testing.T) {
	fenced := false
	release, err := bindDarwin(t.Context(), func() { fenced = true }, func() { t.Error("failure woke old owner") }, Options{}, func(uintptr) uintptr { return 0 }, func(uintptr) { t.Error("failed registration detached invalid handle") })
	if release != nil || !fenced || !errors.Is(err, ErrUnavailable) {
		t.Fatal("registration failure did not disable managed eligibility", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fenced = false
	release, err = bindDarwin(ctx, func() { fenced = true }, func() {}, Options{}, func(uintptr) uintptr { t.Error("ended context registered native bridge"); return 0 }, func(uintptr) {})
	if release != nil || !fenced || !errors.Is(err, ErrUnavailable) {
		t.Fatal("ended context did not fence", err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	fenced, detached := false, false
	release, err = bindDarwin(ctx, func() { fenced = true }, func() {}, Options{}, func(uintptr) uintptr {
		cancel()
		return 7
	}, func(uintptr) {
		if !fenced {
			t.Error("registration cancellation detached before fencing")
		}
		detached = true
	})
	if release != nil || !fenced || !detached || !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancellation during registration returned eligibility", err)
	}
}

func TestDarwinNativeCallbackAcknowledgesOnlyAfterFence(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("native fixture requires clang")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "darwin-power-ack-fixture")
	cmd := exec.CommandContext(ctx, clang, "-fblocks", "-Wall", "-Werror", "-Wno-unused-parameter", "-framework", "CoreFoundation", "-framework", "IOKit", "testdata/darwin_ack_fixture.m", "-o", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native fixture compile: %v\n%s", err, out)
	}
	if out, err := exec.CommandContext(ctx, binary).CombinedOutput(); err != nil {
		t.Fatalf("native fixture: %v\n%s", err, out)
	}
}

// This opt-in census only registers/deregisters the hook. It causes no power
// transition and never owns a model, service or worker process.
func TestDarwinNativeRegistrationCensus(t *testing.T) {
	if os.Getenv("CAELIS_BOT_POWER_REGISTRATION_CENSUS") != "1" {
		t.Skip("opt-in registration census")
	}
	for i := 0; i < 10; i++ {
		var fenced atomic.Bool
		release, err := Bind(t.Context(), func() { fenced.Store(true) }, func() { fenced.Store(true) })
		if err != nil {
			t.Fatal(err)
		}
		release()
		release()
		if !fenced.Load() {
			t.Fatal("release did not fence native owner")
		}
	}
}
