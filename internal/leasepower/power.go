// Package leasepower binds Linux logind sleep preparation to the managed
// runtime's native admission fence. It never starts services or resumes leases.
package leasepower

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const MinimumDelay = 5 * time.Second
const callTimeout = 5 * time.Second
const manager = "org.freedesktop.login1.Manager"
const service = "org.freedesktop.login1"
const managerPath = dbus.ObjectPath("/org/freedesktop/login1")
const busService = "org.freedesktop.DBus"
const busPath = dbus.ObjectPath("/org/freedesktop/DBus")

var ErrUnavailable = errors.New("native managed sleep fencing unavailable")

// UnavailableError supplies a capability reason. A caller must reject managed
// eligibility rather than starting without the protecting delay inhibitor.
type UnavailableError struct {
	Reason string
	Cause  error
}

func (e *UnavailableError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrUnavailable, e.Reason, e.Cause)
	}
	return fmt.Sprintf("%s: %s", ErrUnavailable, e.Reason)
}
func (e *UnavailableError) Unwrap() error        { return e.Cause }
func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

type Options struct {
	// MinimumDelay may raise the required logind budget, never lower the native
	// owner's four-second native freeze/kill confirmation plus delivery margin.
	// No host setting changes.
	MinimumDelay time.Duration
}

// Bind installs a private logind subscription and holds a sleep delay inhibitor
// before returning success. Suspend must synchronously fence and hard-stop the
// native owner within four seconds, leaving a delivery margin in the required
// five-second logind budget. Wake must revoke ownership, never resume it.
// Callbacks are serialized and must not call the returned release function.
// Cancellation or release also calls suspend before closing the inhibitor.
// Only logind-mediated sleep is covered; kernel/privileged bypass is outside it.
func Bind(ctx context.Context, suspend, wake func()) (func(), error) {
	return BindWithOptions(ctx, suspend, wake, Options{})
}

// The narrow port permits deterministic D-Bus fixtures without a system bus or
// actual host sleep. Production uses the pinned native godbus implementation.
type connection interface {
	Call(context.Context, string, dbus.ObjectPath, string, ...any) ([]any, error)
	Subscribe(context.Context, chan<- *dbus.Signal, ...dbus.MatchOption) error
	SupportsUnixFDs() bool
	Done() <-chan struct{}
	Close() error
}

func unavailable(reason string, cause error) error {
	return &UnavailableError{Reason: reason, Cause: cause}
}

func checkedOptions(suspend, wake func(), opts Options) (Options, error) {
	if suspend == nil || wake == nil {
		return opts, unavailable("native suspend and ownership-revoking wake callbacks are required", nil)
	}
	if opts.MinimumDelay == 0 {
		opts.MinimumDelay = MinimumDelay
	}
	if opts.MinimumDelay < MinimumDelay {
		return opts, unavailable("requested delay is shorter than the native stop budget", nil)
	}
	return opts, nil
}

func method(ctx context.Context, c connection, dest string, p dbus.ObjectPath, name string, args ...any) ([]any, error) {
	bounded, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.Call(bounded, dest, p, name, args...)
}

func acquire(ctx context.Context, c connection, owner string) (*os.File, error) {
	body, err := method(ctx, c, owner, managerPath, manager+".Inhibit", "sleep", "Caelis Bot", "Fence the managed native owner before sleep", "delay")
	if err != nil {
		return nil, err
	}
	if len(body) != 1 {
		return nil, errors.New("logind returned an invalid inhibitor descriptor")
	}
	fd, ok := body[0].(dbus.UnixFD)
	if !ok || fd < 0 {
		return nil, errors.New("logind returned no Unix inhibitor descriptor")
	}
	// Inherited copies would keep the inhibitor alive after our acknowledgement.
	markCloseOnExec(int(fd))
	file := os.NewFile(uintptr(fd), "logind-sleep-inhibitor")
	if file == nil {
		return nil, errors.New("invalid logind inhibitor descriptor")
	}
	if _, err := file.Stat(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func initialState(ctx context.Context, c connection, owner string, minimum time.Duration) error {
	body, err := method(ctx, c, owner, managerPath, "org.freedesktop.DBus.Properties.GetAll", manager)
	if err != nil {
		return err
	}
	if len(body) != 1 {
		return errors.New("logind properties unavailable")
	}
	properties, ok := body[0].(map[string]dbus.Variant)
	if !ok {
		return errors.New("logind properties have an unsupported type")
	}
	delay, ok := properties["InhibitDelayMaxUSec"].Value().(uint64)
	if !ok || delay < uint64(minimum/time.Microsecond) {
		return fmt.Errorf("logind delay budget must be at least %s", minimum)
	}
	preparing, ok := properties["PreparingForSleep"].Value().(bool)
	if !ok {
		return errors.New("logind sleep preparation state unavailable")
	}
	if preparing {
		return errors.New("logind is already preparing for sleep")
	}
	return nil
}

func bindConnection(ctx context.Context, c connection, suspend, wake func(), opts Options) (release func(), err error) {
	opts, err = checkedOptions(suspend, wake, opts)
	if err != nil {
		if suspend != nil {
			suspend()
		}
		_ = c.Close()
		return nil, err
	}
	var inhibitor *os.File
	// Even startup failures stop a possibly assembled native owner before an
	// acquired inhibitor is released. Success transfers cleanup to the watcher.
	defer func() {
		if err != nil {
			suspend()
			if inhibitor != nil {
				_ = inhibitor.Close()
			}
			_ = c.Close()
		}
	}()
	if !c.SupportsUnixFDs() {
		return nil, unavailable("system bus cannot transfer Unix inhibitor descriptors", nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, unavailable("binding context ended", err)
	}
	body, err := method(ctx, c, busService, busPath, busService+".GetNameOwner", service)
	if err != nil {
		return nil, unavailable("logind has no system bus owner", err)
	}
	if len(body) != 1 {
		return nil, unavailable("logind owner reply is invalid", nil)
	}
	owner, ok := body[0].(string)
	if !ok || len(owner) < 2 || owner[0] != ':' {
		return nil, unavailable("logind owner is not a unique bus name", nil)
	}
	signals := make(chan *dbus.Signal, 16)
	// Subscribe first, then acquire and read PreparingForSleep to close the
	// startup race. Calls target the attested unique owner, never a replacement.
	subscribeCtx, cancel := context.WithTimeout(ctx, callTimeout)
	err = c.Subscribe(subscribeCtx, signals, dbus.WithMatchSender(owner), dbus.WithMatchObjectPath(managerPath), dbus.WithMatchInterface(manager), dbus.WithMatchMember("PrepareForSleep"))
	cancel()
	if err != nil {
		return nil, unavailable("logind sleep subscription failed", err)
	}
	subscribeCtx, cancel = context.WithTimeout(ctx, callTimeout)
	err = c.Subscribe(subscribeCtx, signals, dbus.WithMatchSender(busService), dbus.WithMatchObjectPath(busPath), dbus.WithMatchInterface(busService), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, service))
	cancel()
	if err != nil {
		return nil, unavailable("logind owner-loss subscription failed", err)
	}
	inhibitor, err = acquire(ctx, c, owner)
	if err != nil {
		return nil, unavailable("logind sleep delay inhibitor denied or unavailable", err)
	}
	if err = initialState(ctx, c, owner, opts.MinimumDelay); err != nil {
		return nil, unavailable("logind cannot cover the native stop budget", err)
	}
	life, cancelLife := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer c.Close()
		defer func() {
			suspend()
			if inhibitor != nil {
				_ = inhibitor.Close()
			}
		}()
		for {
			select {
			case <-life.Done():
				return
			case <-c.Done():
				return
			case signal, open := <-signals:
				if !open || signal == nil {
					return
				}
				if signal.Sender == busService && signal.Path == busPath && signal.Name == busService+".NameOwnerChanged" {
					if len(signal.Body) != 3 {
						return
					}
					if name, ok := signal.Body[0].(string); !ok || name == service {
						return
					}
					continue
				}
				if signal.Sender != owner || signal.Path != managerPath || signal.Name != manager+".PrepareForSleep" {
					continue
				}
				if len(signal.Body) != 1 {
					return
				}
				preparing, ok := signal.Body[0].(bool)
				if !ok {
					return
				}
				if preparing {
					// This is the synchronous preparation acknowledgement: native
					// hard-stop completes before the sole descriptor is closed.
					suspend()
					if inhibitor != nil {
						if err := inhibitor.Close(); err != nil {
							inhibitor = nil
							return
						}
						inhibitor = nil
					}
				} else {
					wake() // old ownership remains revoked, including failed sleep
					if inhibitor == nil {
						fresh, acquireErr := acquire(life, c, owner)
						if acquireErr != nil {
							return
						}
						inhibitor = fresh
						if err := initialState(life, c, owner, opts.MinimumDelay); err != nil {
							return
						}
					}
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(cancelLife); <-done }, nil
}
