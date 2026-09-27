package taskterminal

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Observed state is independent of a user's requested destination. Unknown is
// deliberately distinct from Closed: uncertainty never authorizes a new client.
type WindowState string

const (
	WindowUnknown    WindowState = "unknown"
	WindowClosed     WindowState = "closed"
	WindowCollapsed  WindowState = "collapsed"
	WindowBackground WindowState = "background"
	WindowForeground WindowState = "foreground"
)

type WindowObservation struct {
	State           WindowState
	AppActive       bool
	Settled         bool
	CanCollapse     bool
	InputEpoch      uint64
	ClientEnded     bool // Proven receipt-client exit, independent of GUI lifetime.
	NeedsConnection bool // Previous unclaimed script was revoked; fresh input may retry.
}
type WindowCommand string

const (
	// ShowWindow requests restoration and foreground ownership together. The
	// adapter must not wait for the restore animation before requesting focus.
	ShowWindow     WindowCommand = "show"
	FocusWindow    WindowCommand = "focus"
	CollapseWindow WindowCommand = "collapse"
)

// ControlledWindow submits one explicit native operation at a time. Observe
// supplies evidence, never the last requested value. Apply success is acceptance.
type ControlledWindow interface {
	Window
	Observe(context.Context) (WindowObservation, error)
	Apply(context.Context, WindowCommand) error
}
type WindowTransition string

const (
	WindowReconciling WindowTransition = "reconciling"
	WindowOpening     WindowTransition = "opening"
	WindowShowing     WindowTransition = "showing"
	WindowCollapsing  WindowTransition = "collapsing"
	WindowClosing     WindowTransition = "closing"
	WindowIdle        WindowTransition = "idle"
	WindowUncertain   WindowTransition = "uncertain"
)

// ErrWindowObservationPending means the OS could not read during a transition.
// It authorizes bounded read-only reconciliation, never mutation retries.
var ErrWindowObservationPending = errors.New("terminal window observation temporarily unavailable")

var ErrWindowUnsettled = errors.New("terminal window transition is unconfirmed")
var ErrWindowSuperseded = errors.New("terminal window request superseded by user input")

type windowGoal bool

const (
	goalShown     windowGoal = true
	goalCollapsed windowGoal = false
)

// windowController is one serial actor per binding. Concurrent clicks update
// the destination while the owner waits for native evidence. There is no queue
// of stale toggle commands and no global operation mutex.
type windowController struct {
	mu                           sync.Mutex
	window                       ControlledWindow
	open                         func(context.Context) (ControlledWindow, error)
	inputEpoch                   func() uint64
	changed                      func(WindowTransition, WindowState, error, uint64)
	running                      bool
	closed                       bool
	goal                         windowGoal
	goalKnown                    bool
	extraClicks                  uint64
	revision                     uint64
	completedRevision            uint64
	eventRevision                uint64
	input                        uint64
	cancel                       context.CancelFunc
	done                         chan struct{}
	before                       func(context.Context, ControlledWindow) error
	observed                     WindowObservation
	timeout, interval, stableFor time.Duration
}

func (c *windowController) durations() (time.Duration, time.Duration, time.Duration) {
	timeout, interval, stable := c.timeout, c.interval, c.stableFor
	if timeout == 0 {
		timeout = 4 * time.Second
	}
	if interval == 0 {
		interval = 40 * time.Millisecond
	}
	if stable == 0 {
		stable = 120 * time.Millisecond
	}
	return timeout, interval, stable
}
func (c *windowController) notify(phase WindowTransition, err error) {
	c.mu.Lock()
	c.eventRevision++
	revision := c.eventRevision
	state := c.observed.State
	callback := c.changed
	c.mu.Unlock()
	if callback != nil {
		callback(phase, state, err, revision)
	}
}

// A nil explicit goal means a click; a non-nil one is an idempotent show/drop.
func (c *windowController) request(ctx context.Context, explicit *windowGoal, before func(context.Context, ControlledWindow) error) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return context.Canceled
	}
	if c.inputEpoch != nil {
		c.input = c.inputEpoch()
	}
	c.revision++
	if c.running {
		if explicit != nil {
			c.goal = *explicit
			c.goalKnown = true
		} else if c.goalKnown {
			c.goal = !c.goal
		} else {
			c.extraClicks++
		}
		if before != nil {
			c.before = before
		}
		c.mu.Unlock()
		return nil // Accepted intent; only the owner publishes completion.
	}
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	c.running = true
	c.goalKnown = explicit != nil
	c.extraClicks = 0
	c.before = before
	if explicit != nil {
		c.goal = *explicit
	}
	c.mu.Unlock()
	c.notify(WindowReconciling, nil)
	err := c.run(ctx, true)
	c.mu.Lock()
	if errors.Is(err, ErrWindowSuperseded) && c.observed.InputEpoch <= c.input && ctx.Err() == nil {
		err = nil
	}
	for err == nil && c.completedRevision != c.revision && c.goalKnown {
		c.mu.Unlock()
		err = c.run(ctx, false)
		c.mu.Lock()
	}
	// Request admission and completion are atomic. A subsequent click starts a
	// fresh observation; no old completion can clear a newer request's busy state.
	c.cancel()
	c.cancel = nil
	c.running = false
	c.goalKnown = false
	c.before = nil
	done := c.done
	phase := WindowIdle
	if err != nil && !errors.Is(err, ErrWindowSuperseded) && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrWindowOpenCancelled) && !errors.Is(err, ErrWindowNotConnected) {
		phase = WindowUncertain
	}
	state := c.observed.State
	c.eventRevision++
	eventRevision := c.eventRevision
	callback := c.changed
	close(done)
	c.mu.Unlock()
	if callback != nil {
		callback(phase, state, err, eventRevision)
	}
	if errors.Is(err, ErrWindowSuperseded) {
		return nil
	}
	return err
}
func (c *windowController) read(ctx context.Context) (WindowObservation, error) {
	c.mu.Lock()
	w := c.window
	c.mu.Unlock()
	if w == nil {
		o := WindowObservation{State: WindowClosed, Settled: true}
		c.mu.Lock()
		c.observed = o
		c.mu.Unlock()
		return o, nil
	}
	o, err := w.Observe(ctx)
	if err == nil {
		c.mu.Lock()
		c.observed = o
		input := c.input
		c.mu.Unlock()
		if input != 0 && o.InputEpoch > input {
			return o, ErrWindowSuperseded
		}
	} else {
		c.mu.Lock()
		c.observed = WindowObservation{State: WindowUnknown, InputEpoch: o.InputEpoch}
		c.mu.Unlock()
	}
	return o, err
}
func (c *windowController) reconcile(ctx context.Context) (WindowObservation, error) {
	parent := ctx
	timeout, interval, _ := c.durations()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		o, err := c.read(ctx)
		if !errors.Is(err, ErrWindowObservationPending) {
			return o, err
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return o, parent.Err()
			}
			return o, ErrWindowUnsettled
		case <-tick.C:
		}
	}
}
func (c *windowController) run(ctx context.Context, allowOpen bool) error {
	o, err := c.reconcile(ctx)
	if err != nil {
		return err
	}
	if o.State == WindowUnknown {
		return ErrWindowIdentity
	}
	// A fresh click may reconnect after client exit or a revoked unclaimed
	// script. Prefer the same owned application; never reconnect in flight.
	if o.State == WindowClosed || o.ClientEnded || o.NeedsConnection {
		if !allowOpen {
			return ErrWindowIdentity
		}
		c.notify(WindowOpening, nil)
		w, err := c.open(ctx)
		if w != nil {
			c.mu.Lock()
			c.window = w
			c.mu.Unlock()
		}
		if err != nil {
			return err
		}
		if w == nil {
			c.mu.Lock()
			c.completedRevision = c.revision
			c.mu.Unlock()
			return nil
		}
		c.mu.Lock()
		if !c.goalKnown {
			c.goal = goalShown
			c.goalKnown = true
			if c.extraClicks%2 == 1 {
				c.goal = !c.goal
			}
		}
		c.mu.Unlock()
		o, err = c.reconcile(ctx)
		if err != nil {
			return err
		}
	} else {
		c.mu.Lock()
		if !c.goalKnown {
			c.goal = windowGoal(o.State != WindowForeground || !o.CanCollapse)
			c.goalKnown = true
			if c.extraClicks%2 == 1 {
				c.goal = !c.goal
			}
		}
		c.mu.Unlock()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.mu.Lock()
		goal, rev, before, w := c.goal, c.revision, c.before, c.window
		c.before = nil
		c.mu.Unlock()
		if before != nil {
			if err := before(ctx, w); err != nil {
				return err
			}
			o, err = c.reconcile(ctx)
			if err != nil {
				return err
			}
		}
		timeout, _, _ := c.durations()
		stepCtx, cancel := context.WithTimeout(ctx, timeout)
		if goal == goalShown {
			c.notify(WindowShowing, nil)
			err = c.show(stepCtx, o)
		} else {
			c.notify(WindowCollapsing, nil)
			err = c.collapse(stepCtx, o)
		}
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return ErrWindowUnsettled
			}
			return err
		}
		c.mu.Lock()
		unchanged := rev == c.revision && c.before == nil
		next := c.observed
		c.mu.Unlock()
		if unchanged {
			c.mu.Lock()
			c.completedRevision = rev
			c.mu.Unlock()
			return nil
		}
		o = next
	}
}
func (c *windowController) apply(ctx context.Context, command WindowCommand) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Reconcile user intervention before each mutation, including the focus step
	// after a restore event. Never complete an old request by stealing focus back.
	o, err := c.reconcile(ctx)
	if err != nil {
		return err
	}
	if o.State == WindowClosed {
		return ErrWindowIdentity
	}
	c.mu.Lock()
	w := c.window
	c.mu.Unlock()
	return w.Apply(ctx, command)
}
func (c *windowController) await(ctx context.Context, predicate func(WindowObservation) bool, stable bool) (WindowObservation, error) {
	_, interval, stableFor := c.durations()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var since time.Time
	for {
		o, err := c.read(ctx)
		if errors.Is(err, ErrWindowObservationPending) {
			since = time.Time{}
			select {
			case <-ctx.Done():
				return o, ctx.Err()
			case <-tick.C:
				continue
			}
		}
		if err != nil {
			return o, err
		}
		if o.State == WindowClosed {
			return o, ErrWindowIdentity
		}
		if predicate(o) {
			if !stable {
				return o, nil
			}
			if since.IsZero() {
				since = time.Now()
			}
			if time.Since(since) >= stableFor {
				return o, nil
			}
		} else {
			since = time.Time{}
		}
		select {
		case <-ctx.Done():
			return o, ctx.Err()
		case <-tick.C:
		}
	}
}
func (c *windowController) show(ctx context.Context, o WindowObservation) error {
	if o.State == WindowCollapsed {
		// One display request owns both restoration and foreground acquisition.
		// Do not append a late focus/raise after the animation: a background
		// restoration followed by a second ordering transaction visibly flashes.
		if err := c.apply(ctx, ShowWindow); err != nil {
			return err
		}
	} else if o.State != WindowForeground {
		if err := c.apply(ctx, FocusWindow); err != nil {
			return err
		}
	}
	_, err := c.await(ctx, func(v WindowObservation) bool { return v.State == WindowForeground && v.Settled }, true)
	return err
}

func (c *windowController) collapse(ctx context.Context, o WindowObservation) error {
	if !o.CanCollapse {
		return c.show(ctx, o)
	}
	if o.State != WindowCollapsed {
		if err := c.apply(ctx, CollapseWindow); err != nil {
			if errors.Is(err, ErrWindowUnsupported) {
				return nil
			} // Optional enhancement; retain open/focus.
			return err
		}
	}
	_, err := c.await(ctx, func(v WindowObservation) bool { return v.State == WindowCollapsed && v.Settled }, false)
	return err
}
func (c *windowController) stop(permanent bool) {
	c.mu.Lock()
	if permanent {
		c.closed = true
	}
	if c.cancel != nil {
		c.cancel()
	}
	done := c.done
	running := c.running
	c.mu.Unlock()
	if running {
		<-done
	}
}
