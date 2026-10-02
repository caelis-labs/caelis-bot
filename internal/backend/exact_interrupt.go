package backend

import (
	"context"
	"errors"
)

// exactTurnInterrupter checks the observed native target under its mutation
// lock before invoking the callback. A stale request must not revoke a newer
// turn's desktop authority or interrupt whichever turn happens to be current.
type exactTurnInterrupter interface {
	InterruptTurn(context.Context, string, func()) error
}

func (s *Service) ExactInterruptAvailable() bool {
	_, ok := s.engine.(exactTurnInterrupter)
	return ok
}

func (s *Service) InterruptTurn(ctx context.Context, expectedNativeTurn string) error {
	if expectedNativeTurn == "" {
		return errors.New("interrupt requires the observed turn target")
	}
	engine, ok := s.engine.(exactTurnInterrupter)
	if !ok {
		return errors.New("exact turn interruption is unavailable")
	}
	s.mu.Lock()
	before := s.beforeInterrupt
	s.mu.Unlock()
	return engine.InterruptTurn(ctx, expectedNativeTurn, before)
}
