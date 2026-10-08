package taskterminal

import (
	"context"
	"errors"
)

var ErrWindowActivation = errors.New("terminal window could not be brought to the foreground")

var ErrWindowControl = errors.New("terminal window operation failed")

var ErrWindowPermission = errors.New("terminal window control permission is required")
var ErrWindowUnsupported = errors.New("this terminal adapter does not support the window operation")
var ErrWindowIdentity = errors.New("terminal window identity could not be confirmed")
var ErrWindowClosePending = errors.New("terminal instance is still running after a normal quit request")
var ErrWindowCloseCancelled = errors.New("terminal close cancelled by user")
var ErrWindowOpenCancelled = errors.New("terminal opening cancelled")
var ErrWindowNotConnected = errors.New("terminal did not start the connection")

// DocumentWindow is optional. It opens a document in the owned application,
// never types into an existing shell or chooses a window/tab by title.
type DocumentWindow interface {
	OpenDocument(context.Context, string) error
	DocumentResult(context.Context) (complete bool, err error)
}

// Window owns an opaque native terminal handle (a dedicated application instance
// on macOS). Hosts implement optional actions independently; attaching does not
// imply collapse, placement or capture support.
type Window interface {
	Confirm(context.Context, int) error
	Release()
}

func NewManaged(directory string, open func(context.Context, string) (Window, error)) *Launcher {
	return &Launcher{directory: directory, openWindow: open, windows: map[string]Window{}}
}

// Explicit operations have priority over optional observations. Hold admission
// while acquiring mu so a new observation cannot slip in after cancellation.
// Observations release mu without acquiring observationMu (no lock inversion).
func (l *Launcher) lockOperation() {
	l.observationMu.Lock()
	if l.observationCancel != nil {
		l.observationCancel()
		l.observationCancel = nil
	}
	l.mu.Lock()
	l.observationMu.Unlock()
}

// Close releases observation handles and already-read revoked scripts. It never
// closes terminal windows or stops their clients; Runtime owns that decision.
func (l *Launcher) Close() {
	l.lockOperation()
	defer l.mu.Unlock()
	for id, w := range l.windows {
		_ = l.cleanReadAttempts(id)
		w.Release()
		delete(l.windows, id)
	}
}

func (l *Launcher) Dismiss(ctx context.Context, id string) error {
	l.lockOperation()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	w := l.windows[id]
	if w == nil {
		if l.unmanaged[id] {
			return ErrWindowIdentity
		}
		return nil
	}
	a, ok := w.(interface{ Dismiss(context.Context) error })
	if !ok {
		return ErrWindowUnsupported
	}
	if err := a.Dismiss(ctx); err != nil {
		return err
	}
	if err := l.cleanReadAttempts(id); err != nil {
		return err
	}
	w.Release()
	delete(l.windows, id)
	return nil
}
