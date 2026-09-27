package taskterminal

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observedWindow struct {
	mu          sync.Mutex
	observation WindowObservation
	commands    []WindowCommand
	submitted   chan WindowCommand
	apply       func(WindowCommand) error
	read        func() error
}

func (w *observedWindow) Confirm(context.Context, int) error { return nil }
func (w *observedWindow) Release()                           {}
func (w *observedWindow) Observe(context.Context) (WindowObservation, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.read != nil {
		if err := w.read(); err != nil {
			return WindowObservation{State: WindowUnknown}, err
		}
	}
	return w.observation, nil
}
func (w *observedWindow) Apply(_ context.Context, cmd WindowCommand) error {
	w.mu.Lock()
	w.commands = append(w.commands, cmd)
	f := w.apply
	w.mu.Unlock()
	if w.submitted != nil {
		w.submitted <- cmd
	}
	if f != nil {
		return f(cmd)
	}
	return nil
}
func (w *observedWindow) set(f func(*WindowObservation)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f(&w.observation)
}
func (w *observedWindow) calls() []WindowCommand {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]WindowCommand{}, w.commands...)
}
func controllerFixture(state WindowState) (*windowController, *observedWindow) {
	w := &observedWindow{observation: WindowObservation{State: state, Settled: true, CanCollapse: true, AppActive: state == WindowForeground}, submitted: make(chan WindowCommand, 20)}
	return &windowController{window: w, timeout: 400 * time.Millisecond, interval: time.Millisecond, stableFor: 2 * time.Millisecond}, w
}
func nextCommand(t *testing.T, w *observedWindow, want WindowCommand) {
	t.Helper()
	select {
	case got := <-w.submitted:
		if got != want {
			t.Fatalf("command %s, want %s", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("missing %s", want)
	}
}
func noCommand(t *testing.T, w *observedWindow) {
	t.Helper()
	select {
	case c := <-w.submitted:
		t.Fatalf("unexpected mutation %s", c)
	default:
	}
}
func result(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatal("controller did not complete")
		return nil
	}
}
func asyncClick(t *testing.T, c *windowController) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.request(t.Context(), nil, nil) }()
	return done
}
func immediateWindow(w *observedWindow) {
	w.apply = func(cmd WindowCommand) error {
		w.set(func(o *WindowObservation) {
			switch cmd {
			case ShowWindow:
				o.State = WindowForeground
				o.AppActive = true
			case FocusWindow:
				o.State = WindowForeground
				o.AppActive = true
			case CollapseWindow:
				o.State = WindowCollapsed
			}
			o.Settled = true
		})
		return nil
	}
}
func TestControllerShowRequiresBothRestoreEventAndForeground(t *testing.T) {
	c, w := controllerFixture(WindowCollapsed)
	done := asyncClick(t, c)
	nextCommand(t, w, ShowWindow)
	// All windows can be minimized: active application is not a prerequisite.
	// An accepted restore and early property change are not completion evidence.
	w.set(func(o *WindowObservation) { o.State = WindowBackground; o.Settled = false })
	time.Sleep(5 * time.Millisecond)
	noCommand(t, w)
	select {
	case <-done:
		t.Fatal("accepted restore was reported completed")
	default:
	}
	w.set(func(o *WindowObservation) { o.Settled = true })
	time.Sleep(5 * time.Millisecond)
	noCommand(t, w) // A restore event cannot trigger a late corrective focus.
	select {
	case <-done:
		t.Fatal("background restoration was reported completed")
	default:
	}
	w.set(func(o *WindowObservation) { o.State = WindowForeground; o.AppActive = true })
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
}
func TestControllerCoalescesRapidClicksToFinalDestination(t *testing.T) {
	for _, extra := range []int{1, 2, 3, 10} {
		t.Run(time.Duration(extra).String(), func(t *testing.T) {
			c, w := controllerFixture(WindowBackground)
			done := asyncClick(t, c)
			nextCommand(t, w, FocusWindow)
			for range extra {
				if err := c.request(t.Context(), nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			w.mu.Lock()
			w.apply = func(cmd WindowCommand) error {
				w.set(func(o *WindowObservation) {
					if cmd == CollapseWindow {
						o.State = WindowCollapsed
						o.Settled = true
					}
				})
				return nil
			}
			w.mu.Unlock()
			w.set(func(o *WindowObservation) { o.State = WindowForeground; o.AppActive = true })
			if err := result(t, done); err != nil {
				t.Fatal(err)
			}
			want := []WindowCommand{FocusWindow}
			if extra%2 == 1 {
				want = append(want, CollapseWindow)
			}
			if got := w.calls(); !reflect.DeepEqual(got, want) {
				t.Fatalf("calls %v want %v", got, want)
			}
		})
	}
}
func TestControllerForegroundRequiresExactWindowNotJustActiveApp(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	c.timeout = 20 * time.Millisecond
	w.set(func(o *WindowObservation) { o.AppActive = true })
	if err := c.request(t.Context(), nil, nil); !errors.Is(err, ErrWindowUnsettled) {
		t.Fatal(err)
	}
	if got := w.calls(); !reflect.DeepEqual(got, []WindowCommand{FocusWindow}) {
		t.Fatal(got)
	}
}
func TestControllerRespectsNewUserInputDuringShow(t *testing.T) {
	c, w := controllerFixture(WindowCollapsed)
	c.inputEpoch = func() uint64 { return 1 }
	w.set(func(o *WindowObservation) { o.AppActive = true; o.InputEpoch = 1 })
	done := asyncClick(t, c)
	nextCommand(t, w, ShowWindow)
	w.set(func(o *WindowObservation) { o.State = WindowBackground; o.Settled = true; o.InputEpoch = 2 })
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	noCommand(t, w)
}
func TestControllerClosedDuringTransitionNeverReopens(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	opens := 0
	c.open = func(context.Context) (ControlledWindow, error) { opens++; return nil, ErrWindowIdentity }
	done := asyncClick(t, c)
	nextCommand(t, w, FocusWindow)
	w.set(func(o *WindowObservation) { o.State = WindowClosed })
	if err := result(t, done); !errors.Is(err, ErrWindowIdentity) {
		t.Fatal(err)
	}
	if opens != 0 {
		t.Fatal("reopened user-closed window")
	}
	_ = c.request(t.Context(), nil, nil)
	if opens != 1 {
		t.Fatal("fresh closed click should resolve one new client")
	}
}
func TestControllerUnknownAndPermissionDoNotReopen(t *testing.T) {
	for _, err := range []error{ErrWindowPermission, ErrWindowIdentity} {
		c, w := controllerFixture(WindowUnknown)
		w.read = func() error { return err }
		c.open = func(context.Context) (ControlledWindow, error) {
			t.Fatal("reopened uncertain binding")
			return nil, nil
		}
		if got := c.request(t.Context(), nil, nil); !errors.Is(got, err) {
			t.Fatal(got)
		}
	}
}
func TestControllerAdmissionFromCompletionCallbackIsNotLost(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	immediateWindow(w)
	var entered atomic.Bool
	c.changed = func(phase WindowTransition, _ WindowState, _ error, _ uint64) {
		if phase == WindowIdle && entered.CompareAndSwap(false, true) {
			if err := c.request(t.Context(), nil, nil); err != nil {
				t.Error(err)
			}
		}
	}
	if err := c.request(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := w.calls(); !reflect.DeepEqual(got, []WindowCommand{FocusWindow, CollapseWindow}) {
		t.Fatal(got)
	}
}
func TestControllerCancelJoinsOwnerAndFencesLateInput(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	done := asyncClick(t, c)
	nextCommand(t, w, FocusWindow)
	c.stop(true)
	if err := result(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := c.request(t.Context(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	noCommand(t, w)
}
func TestControllerUnsupportedMinimizeUsesFocusOnly(t *testing.T) {
	c, w := controllerFixture(WindowForeground)
	w.set(func(o *WindowObservation) { o.CanCollapse = false })
	if err := c.request(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	noCommand(t, w)
}

func TestOptionalCollapseRefusalKeepsBasicFocusUsable(t *testing.T) {
	c, w := controllerFixture(WindowForeground)
	w.apply = func(command WindowCommand) error {
		if command == CollapseWindow {
			w.set(func(o *WindowObservation) { o.CanCollapse = false })
			return ErrWindowUnsupported
		}
		w.set(func(o *WindowObservation) { o.State = WindowForeground })
		return nil
	}
	if err := c.request(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	w.set(func(o *WindowObservation) { o.State = WindowBackground })
	if err := c.request(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.calls(), []WindowCommand{CollapseWindow, FocusWindow}) {
		t.Fatal(w.calls())
	}
}

func TestControllerTransientReadDuringAnimationDoesNotRepeatMutation(t *testing.T) {
	c, w := controllerFixture(WindowCollapsed)
	w.set(func(o *WindowObservation) { o.AppActive = true })
	done := asyncClick(t, c)
	nextCommand(t, w, ShowWindow)
	var reads atomic.Int32
	w.mu.Lock()
	w.read = func() error { reads.Add(1); return ErrWindowObservationPending }
	w.mu.Unlock()
	for reads.Load() < 3 {
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatal("transient animation read ended transition", err)
	default:
	}
	w.mu.Lock()
	w.read = nil
	w.observation.State = WindowForeground
	w.observation.Settled = true
	w.mu.Unlock()
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	noCommand(t, w)
}
func TestControllerTransientReadReconciliationHasDeadline(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	c.timeout = 10 * time.Millisecond
	w.read = func() error { return ErrWindowObservationPending }
	if err := c.request(t.Context(), nil, nil); !errors.Is(err, ErrWindowUnsettled) {
		t.Fatal(err)
	}
	noCommand(t, w)
}

func TestControllerShowWithoutForegroundTimesOutWithoutCorrectiveFocus(t *testing.T) {
	c, w := controllerFixture(WindowCollapsed)
	c.timeout = 15 * time.Millisecond
	w.apply = func(WindowCommand) error {
		w.set(func(o *WindowObservation) { o.State = WindowBackground; o.Settled = true })
		return nil
	}
	if err := c.request(t.Context(), nil, nil); !errors.Is(err, ErrWindowUnsettled) {
		t.Fatal(err)
	}
	if got := w.calls(); !reflect.DeepEqual(got, []WindowCommand{ShowWindow}) {
		t.Fatalf("late focus must not steal ownership: %v", got)
	}
}
