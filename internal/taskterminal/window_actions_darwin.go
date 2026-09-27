//go:build darwin && cgo

package taskterminal

import "context"

// Point uses AppKit desktop coordinates (origin at the lower left).
type Point struct{ X, Y float64 }
type PreviewSource struct {
	PID      int    `json:"pid"`
	Birth    uint64 `json:"birth"`
	Terminal string `json:"terminal"`
}

func (l *Launcher) Preview(ctx context.Context, id string) (PreviewSource, error) {
	// Never queue a preview behind user input. A later click cancels this
	// read-only request before taking the same native window handle.
	if !l.observationMu.TryLock() {
		return PreviewSource{}, context.Canceled
	}
	if !l.mu.TryLock() {
		l.observationMu.Unlock()
		return PreviewSource{}, context.Canceled
	}
	ctx, cancel := context.WithCancel(ctx)
	l.observationCancel = cancel
	l.observationMu.Unlock()
	defer l.mu.Unlock()
	defer cancel()
	if a, ok := l.windows[id].(interface {
		Preview(context.Context) (PreviewSource, error)
	}); ok {
		return a.Preview(ctx)
	}
	return PreviewSource{}, ErrWindowIdentity
}
