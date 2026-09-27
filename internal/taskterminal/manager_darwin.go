//go:build darwin && cgo

package taskterminal

import (
	"context"
	"errors"
)

func (m *WindowManager) Place(ctx context.Context, id string, p Point) error {
	e, _, err := m.entryForInput(ctx, id)
	if err != nil {
		return err
	}
	shown := goalShown
	return e.controller.request(ctx, &shown, func(ctx context.Context, w ControlledWindow) error {
		if move, ok := w.(interface {
			Move(context.Context, Point) error
		}); ok {
			err := move.Move(ctx, p)
			if !errors.Is(err, ErrWindowUnsupported) {
				return err
			}
			return nil
		}
		return nil // Placement is optional; opening/foregrounding still succeeds.
	})
}
func (b *controlledBinding) Move(ctx context.Context, p Point) error {
	b.launcher.lockOperation()
	defer b.launcher.mu.Unlock()
	if b.launcher.windows[b.id] != b.window {
		return ErrWindowIdentity
	}
	if move, ok := b.window.(interface {
		Move(context.Context, Point) error
	}); ok {
		return move.Move(ctx, p)
	}
	return ErrWindowUnsupported
}
func (m *WindowManager) Preview(ctx context.Context, id string) (PreviewSource, error) {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e == nil {
		return PreviewSource{}, ErrWindowIdentity
	}
	e.controller.mu.Lock()
	ready := !e.controller.running && !e.controller.closed && e.controller.observed.State == WindowForeground
	e.controller.mu.Unlock()
	if !ready {
		return PreviewSource{}, context.Canceled
	}
	return e.launcher.Preview(ctx, id)
}
