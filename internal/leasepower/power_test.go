//go:build !windows

package leasepower

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type fakeInhibitor struct {
	closed chan struct{}
}

type fakeBus struct {
	t                                                       *testing.T
	mu                                                      sync.Mutex
	signals                                                 chan<- *dbus.Signal
	leases                                                  []*fakeInhibitor
	acquired                                                chan int
	done                                                    chan struct{}
	closeOnce                                               sync.Once
	fdSupport                                               bool
	delay                                                   uint64
	preparing                                               bool
	ownerErr, errorSubscribe, errorInhibit, errorProperties error
	propertiesOverride                                      []any
	calls                                                   []string
}

func newFakeBus(t *testing.T) *fakeBus {
	f := &fakeBus{t: t, acquired: make(chan int, 8), done: make(chan struct{}), fdSupport: true, delay: uint64(MinimumDelay / time.Microsecond)}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
func (f *fakeBus) SupportsUnixFDs() bool { return f.fdSupport }
func (f *fakeBus) Done() <-chan struct{} { return f.done }
func (f *fakeBus) Close() error          { f.closeOnce.Do(func() { close(f.done) }); return nil }
func (f *fakeBus) Subscribe(ctx context.Context, ch chan<- *dbus.Signal, opts ...dbus.MatchOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = ch
	f.calls = append(f.calls, "subscribe")
	if len(opts) < 4 {
		return errors.New("missing strict match rules")
	}
	return f.errorSubscribe
}
func (f *fakeBus) Call(ctx context.Context, dest string, p dbus.ObjectPath, name string, args ...any) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	switch name {
	case busService + ".GetNameOwner":
		if dest != busService || p != busPath || !reflect.DeepEqual(args, []any{service}) {
			return nil, errors.New("invalid owner lookup")
		}
		return []any{":1.42"}, f.ownerErr
	case manager + ".Inhibit":
		if dest != ":1.42" || p != managerPath || !reflect.DeepEqual(args, []any{"sleep", "Caelis Bot", "Fence the managed native owner before sleep", "delay"}) {
			return nil, errors.New("invalid native inhibitor call")
		}
		if f.errorInhibit != nil {
			return nil, f.errorInhibit
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		fd, err := syscall.Dup(int(writer.Fd()))
		_ = writer.Close()
		if err != nil {
			_ = reader.Close()
			return nil, err
		}
		lease := &fakeInhibitor{closed: make(chan struct{})}
		f.leases = append(f.leases, lease)
		go func() { var b [1]byte; _, _ = reader.Read(b[:]); _ = reader.Close(); close(lease.closed) }()
		f.acquired <- len(f.leases)
		return []any{dbus.UnixFD(fd)}, nil
	case "org.freedesktop.DBus.Properties.GetAll":
		if dest != ":1.42" || p != managerPath || !reflect.DeepEqual(args, []any{manager}) {
			return nil, errors.New("invalid property read")
		}
		if f.propertiesOverride != nil {
			return f.propertiesOverride, f.errorProperties
		}
		return []any{map[string]dbus.Variant{"InhibitDelayMaxUSec": dbus.MakeVariant(f.delay), "PreparingForSleep": dbus.MakeVariant(f.preparing)}}, f.errorProperties
	default:
		return nil, fmt.Errorf("unexpected D-Bus call: %s", name)
	}
}
func (f *fakeBus) emit(signal *dbus.Signal) {
	f.mu.Lock()
	ch := f.signals
	f.mu.Unlock()
	select {
	case ch <- signal:
	case <-f.done:
	}
}
func (f *fakeBus) lease(n int) *fakeInhibitor { f.mu.Lock(); defer f.mu.Unlock(); return f.leases[n-1] }
func sleepSignal(b bool) *dbus.Signal {
	return &dbus.Signal{Sender: ":1.42", Path: managerPath, Name: manager + ".PrepareForSleep", Body: []any{b}}
}
func waitFor(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture timed out")
	}
}
func waitAcquire(t *testing.T, f *fakeBus, want int) {
	t.Helper()
	select {
	case n := <-f.acquired:
		if n != want {
			t.Fatalf("inhibitor generation %d, want %d", n, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fixture inhibitor timeout")
	}
}

func TestDelayInhibitorWaitsForNativeStopAndReacquiresAfterWake(t *testing.T) {
	f := newFakeBus(t)
	stopping, finish, stopped, woke := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var stopOnce, wakeOnce sync.Once
	release, err := bindConnection(t.Context(), f, func() { stopOnce.Do(func() { close(stopping); <-finish; close(stopped) }) }, func() {
		select {
		case <-stopped:
		default:
			t.Error("wake preceded native stop confirmation")
		}
		wakeOnce.Do(func() { close(woke) })
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitAcquire(t, f, 1)
	f.mu.Lock()
	calls := append([]string{}, f.calls...)
	f.mu.Unlock()
	want := []string{busService + ".GetNameOwner", "subscribe", "subscribe", manager + ".Inhibit", "org.freedesktop.DBus.Properties.GetAll"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unsafe binding order: %v", calls)
	}
	f.emit(sleepSignal(true))
	waitFor(t, stopping)
	select {
	case <-f.lease(1).closed:
		t.Fatal("sleep acknowledged before native hard-stop completed")
	default:
	}
	// Wake can already be queued, but cannot run before stop and FD release.
	f.emit(sleepSignal(false))
	close(finish)
	waitFor(t, stopped)
	waitFor(t, f.lease(1).closed)
	waitFor(t, woke)
	waitAcquire(t, f, 2)
	select {
	case <-f.lease(2).closed:
		t.Fatal("new delay inhibitor was released while awake")
	default:
	}
	release()
	waitFor(t, f.lease(2).closed)
	release() // idempotent, no restart, no descriptor retained
}

func TestBindingFailuresFenceOwnerBeforeReturningUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*fakeBus)
		reason    string
	}{
		{"no-fd", func(f *fakeBus) { f.fdSupport = false }, "Unix"},
		{"no-logind", func(f *fakeBus) { f.ownerErr = errors.New("service absent") }, "owner"},
		{"subscription-denied", func(f *fakeBus) { f.errorSubscribe = errors.New("denied") }, "subscription"},
		{"inhibit-denied", func(f *fakeBus) { f.errorInhibit = errors.New("policy denied") }, "inhibitor"},
		{"short-delay", func(f *fakeBus) { f.delay = 1 }, "budget"},
		{"already-sleeping", func(f *fakeBus) { f.preparing = true }, "budget"},
		{"missing-properties", func(f *fakeBus) { f.errorProperties = errors.New("unsupported") }, "budget"},
		{"invalid-properties", func(f *fakeBus) {
			f.propertiesOverride = []any{map[string]dbus.Variant{"InhibitDelayMaxUSec": dbus.MakeVariant("unknown")}}
		}, "budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeBus(t)
			tc.configure(f)
			fenced := false
			release, err := bindConnection(t.Context(), f, func() { fenced = true }, func() { t.Error("bind failure resumed ownership") }, Options{})
			var unavailableErr *UnavailableError
			if release != nil || !fenced || !errors.Is(err, ErrUnavailable) || !errors.As(err, &unavailableErr) || !strings.Contains(unavailableErr.Reason, tc.reason) {
				t.Fatalf("capability failure: fenced=%v release=%v err=%v", fenced, release != nil, err)
			}
			f.mu.Lock()
			leases := append([]*fakeInhibitor{}, f.leases...)
			f.mu.Unlock()
			for _, lease := range leases {
				waitFor(t, lease.closed)
			}
		})
	}
}

func TestBusLossOwnerReplacementAndInvalidNativeSignalsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger func(*fakeBus)
	}{
		{"disconnect", func(f *fakeBus) { _ = f.Close() }},
		{"logind-replaced", func(f *fakeBus) {
			f.emit(&dbus.Signal{Sender: busService, Path: busPath, Name: busService + ".NameOwnerChanged", Body: []any{service, ":1.42", ":1.43"}})
		}},
		{"malformed-native", func(f *fakeBus) { s := sleepSignal(true); s.Body = []any{"true"}; f.emit(s) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeBus(t)
			fenced := make(chan struct{})
			var once sync.Once
			release, err := bindConnection(t.Context(), f, func() { once.Do(func() { close(fenced) }) }, func() { t.Error("failure restored ownership") }, Options{})
			if err != nil {
				t.Fatal(err)
			}
			waitAcquire(t, f, 1)
			tc.trigger(f)
			waitFor(t, fenced)
			waitFor(t, f.lease(1).closed)
			release()
		})
	}
}

func TestForeignSignalsCannotInvokePowerCallbacks(t *testing.T) {
	f := newFakeBus(t)
	suspended := make(chan struct{}, 1)
	woke := make(chan struct{}, 1)
	release, err := bindConnection(t.Context(), f, func() { suspended <- struct{}{} }, func() { woke <- struct{}{} }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitAcquire(t, f, 1)
	for _, s := range []*dbus.Signal{
		{Sender: ":1.99", Path: managerPath, Name: manager + ".PrepareForSleep", Body: []any{true}},
		{Sender: ":1.42", Path: "/foreign", Name: manager + ".PrepareForSleep", Body: []any{true}},
		{Sender: ":1.42", Path: managerPath, Name: manager + ".Foreign", Body: []any{true}},
	} {
		f.emit(s)
	}
	// A legitimate wake is an ordering barrier after all foreign signals.
	f.emit(sleepSignal(false))
	waitFor(t, woke)
	select {
	case <-suspended:
		t.Fatal("foreign signal suspended the owner")
	default:
	}
	release()
	waitFor(t, suspended)
}

func TestWakeReacquisitionFailureKeepsOwnerFenced(t *testing.T) {
	f := newFakeBus(t)
	fenced := make(chan struct{}, 4)
	woke := make(chan struct{}, 1)
	release, err := bindConnection(t.Context(), f, func() { fenced <- struct{}{} }, func() { woke <- struct{}{} }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitAcquire(t, f, 1)
	f.emit(sleepSignal(true))
	waitFor(t, fenced)
	waitFor(t, f.lease(1).closed)
	f.mu.Lock()
	f.errorInhibit = errors.New("denied after wake")
	f.mu.Unlock()
	f.emit(sleepSignal(false))
	waitFor(t, woke)
	waitFor(t, fenced)
	release()
	select {
	case n := <-f.acquired:
		t.Fatalf("failed wake acquired lease %d", n)
	default:
	}
}

func TestCancellationStopsBeforeReleasingInhibitor(t *testing.T) {
	f := newFakeBus(t)
	ctx, cancel := context.WithCancel(t.Context())
	entered, finish := make(chan struct{}), make(chan struct{})
	release, err := bindConnection(ctx, f, func() { close(entered); <-finish }, func() { t.Error("cancel resumed") }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitAcquire(t, f, 1)
	cancel()
	waitFor(t, entered)
	select {
	case <-f.lease(1).closed:
		t.Fatal("cancellation released inhibitor before native fence")
	default:
	}
	close(finish)
	release()
	waitFor(t, f.lease(1).closed)
}
