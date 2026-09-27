//go:build darwin && cgo

package desktop

import (
	"context"
	"log"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

// Only the macOS adapter exposes AppKit placement coordinates.
func (s *Service) openTaskAt(ctx context.Context, id string, point *taskterminal.Point) error {
	if point == nil {
		return s.openTask(ctx, id)
	}
	if !s.taskWindowAllowed(id) {
		return taskterminal.ErrWindowIdentity
	}
	placer, ok := s.taskWindows.(interface {
		Place(context.Context, string, taskterminal.Point) error
	})
	if !ok {
		return taskterminal.ErrWindowUnsupported
	}
	return placer.Place(ctx, id, *point)
}
func (s *Service) observeMacTaskTerminal(ctx context.Context, id string, launcher interface {
	Preview(context.Context, string) (taskterminal.PreviewSource, error)
}) {
	// Do not ask the terminal for preview metadata when capture is unavailable.
	// Preflight only: this must never trigger a screen-recording permission prompt.
	if status := taskSnapshotStatus(); status != "available" {
		log.Printf("Task terminal preview skipped: %s", status)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	source, err := launcher.Preview(ctx, id)
	if err != nil || ctx.Err() != nil {
		// Fixed diagnostic only: do not include runtime targets or terminal text.
		log.Printf("Task terminal preview skipped: window identity unavailable or observation cancelled")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(*macDriver); ok && !s.stopped {
		d.taskSnapshot(id, source)
	}
}
