package taskterminal

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type WindowEvent struct {
	Revision uint64
	Phase    WindowTransition
	State    WindowState
	Err      error
}
type managedEntry struct {
	controller    *windowController
	launcher      *Launcher
	blocked       error
	unmanaged     bool
	dismissCancel context.CancelFunc // Protected by manager.mu.
	dismissDone   chan struct{}
}

// WindowManager owns one controller and one receipt/binding store per task.
// Separate task actors cannot block each other on native events or consent.
type WindowManager struct {
	mu         sync.Mutex
	entries    map[string]*managedEntry
	generation uint64
	stopped    bool
	directory  string
	open       func(context.Context, string) (Window, error)
	resolve    func(context.Context, string) (api.TerminalTarget, error)
	changed    func(string, WindowEvent)
	inputEpoch func() uint64
}

func NewWindowManager(directory string, open func(context.Context, string) (Window, error), resolve func(context.Context, string) (api.TerminalTarget, error), changed func(string, WindowEvent), input func() uint64) *WindowManager {
	return &WindowManager{directory: directory, open: open, resolve: resolve, changed: changed, inputEpoch: input, entries: map[string]*managedEntry{}}
}
func (m *WindowManager) entry(id string) (*managedEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil, context.Canceled
	}
	if e := m.entries[id]; e != nil {
		return e, nil
	}
	m.generation++
	generation := m.generation
	l := NewManaged(m.directory, m.open)
	e := &managedEntry{launcher: l}
	c := &windowController{inputEpoch: m.inputEpoch}
	e.controller = c
	c.changed = func(phase WindowTransition, state WindowState, err error, revision uint64) {
		if m.changed != nil {
			m.changed(id, WindowEvent{Revision: generation<<32 | revision, Phase: phase, State: state, Err: err})
		}
	}
	c.open = func(ctx context.Context) (ControlledWindow, error) {
		if e.blocked != nil {
			return nil, e.blocked
		}
		if e.unmanaged {
			return nil, ErrWindowUnsupported
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		target, err := m.resolve(ctx, id)
		if err != nil {
			return nil, err
		}
		err = l.Open(ctx, id, target)
		l.mu.Lock()
		w := l.windows[id]
		e.unmanaged = l.unmanaged[id]
		l.mu.Unlock()
		var controlled ControlledWindow
		if cw, ok := w.(ControlledWindow); ok {
			controlled = &controlledBinding{launcher: l, id: id, window: cw}
		}
		if w != nil && controlled == nil {
			e.blocked = ErrWindowUnsupported
			return nil, ErrWindowUnsupported
		}
		if err != nil && w == nil && !errors.Is(err, ErrLaunchNotSubmitted) {
			e.blocked = err
		}
		return controlled, err
	}
	m.entries[id] = e
	return e, nil
}
func (m *WindowManager) Click(ctx context.Context, id string) error {
	e, pending, err := m.entryForInput(ctx, id)
	if err != nil {
		return err
	}
	if pending {
		shown := goalShown
		return e.controller.request(ctx, &shown, nil)
	}
	return e.controller.request(ctx, nil, nil)
}

// A click during native confirmation takes over the observation wait and brings
// the instance forward. It never dismisses or accepts the terminal's dialog.
func (m *WindowManager) entryForInput(ctx context.Context, id string) (*managedEntry, bool, error) {
	e, err := m.entry(id)
	if err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	done := e.dismissDone
	if e.dismissCancel != nil {
		e.dismissCancel()
	}
	m.mu.Unlock()
	if done == nil {
		if waiting, retired := e.launcher.retryPending(ctx); waiting {
			if !retired {
				return e, true, nil
			}
			e.controller.mu.Lock()
			openingDone := e.controller.done
			e.controller.mu.Unlock()
			if openingDone != nil {
				select {
				case <-ctx.Done():
					return nil, true, ctx.Err()
				case <-openingDone:
				}
			}
			return e, true, nil
		}
		return e, false, nil
	}
	select {
	case <-ctx.Done():
		return nil, true, ctx.Err()
	case <-done:
	}
	e, err = m.entry(id)
	return e, true, err
}
func (m *WindowManager) Cancel(id string) {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e != nil {
		e.controller.mu.Lock()
		if e.controller.cancel != nil {
			e.controller.cancel()
		}
		e.controller.mu.Unlock()
	}
}
func (m *WindowManager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.dismissCancel != nil {
			e.dismissCancel()
		}
		e.controller.mu.Lock()
		if e.controller.cancel != nil {
			e.controller.cancel()
		}
		e.controller.mu.Unlock()
	}
}
func (m *WindowManager) Close() {
	m.mu.Lock()
	m.stopped = true
	entries := make([]*managedEntry, 0, len(m.entries))
	for _, e := range m.entries {
		if e.dismissCancel != nil {
			e.dismissCancel()
		}
		entries = append(entries, e)
	}
	m.mu.Unlock()
	for _, e := range entries {
		e.controller.stop(true)
		e.launcher.Close()
	}
}
func (m *WindowManager) Dismiss(ctx context.Context, id string) error {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil {
		m.mu.Unlock()
		return nil
	}
	if e.dismissCancel != nil {
		m.mu.Unlock()
		return ErrWindowClosePending
	}
	ctx, cancel := context.WithCancel(ctx)
	e.dismissCancel = cancel
	e.dismissDone = make(chan struct{})
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		e.dismissCancel = nil
		close(e.dismissDone)
		e.dismissDone = nil
		m.mu.Unlock()
	}()
	// Close is also a serialized user operation. Permanent admission fencing
	// prevents a late click reopening the client while the card is being removed.
	e.controller.stop(true)
	e.controller.notify(WindowClosing, nil)
	err := e.launcher.Dismiss(ctx, id)
	if err != nil {
		e.controller.mu.Lock()
		e.controller.closed = false
		e.controller.mu.Unlock()
		phase := WindowUncertain
		if errors.Is(err, ErrWindowCloseCancelled) || errors.Is(err, ErrWindowClosePending) || errors.Is(err, context.Canceled) {
			phase = WindowIdle
		}
		e.controller.notify(phase, err)
		return err
	}
	e.controller.mu.Lock()
	e.controller.observed = WindowObservation{State: WindowClosed, Settled: true}
	e.controller.mu.Unlock()
	e.controller.notify(WindowIdle, nil)
	m.mu.Lock()
	if m.entries[id] == e {
		delete(m.entries, id)
	}
	m.mu.Unlock()
	return nil
}

// All access to a retained native handle shares its lease with optional
// observations. A replaced binding can never receive an old action/callback.
type controlledBinding struct {
	launcher *Launcher
	id       string
	window   ControlledWindow
}

func (b *controlledBinding) Confirm(ctx context.Context, pid int) error {
	return b.window.Confirm(ctx, pid)
}
func (b *controlledBinding) Release() {} // Launcher owns the native handle.
func (b *controlledBinding) Observe(ctx context.Context) (WindowObservation, error) {
	b.launcher.lockOperation()
	defer b.launcher.mu.Unlock()
	if b.launcher.windows[b.id] != b.window {
		return WindowObservation{State: WindowClosed, Settled: true}, nil
	}
	o, err := b.window.Observe(ctx)
	o.NeedsConnection = b.launcher.reconnect[b.id]
	return o, err
}
func (b *controlledBinding) Apply(ctx context.Context, command WindowCommand) error {
	b.launcher.lockOperation()
	defer b.launcher.mu.Unlock()
	if b.launcher.windows[b.id] != b.window {
		return ErrWindowIdentity
	}
	return b.window.Apply(ctx, command)
}
