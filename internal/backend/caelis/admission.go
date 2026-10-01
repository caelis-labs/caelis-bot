package caelis

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"time"
)

func NewOwned(ctx context.Context, opts Options, owner OwnedHostOptions) (*Session, error) {
	h, err := startOwnedHost(ctx, owner)
	if err != nil {
		return nil, err
	}
	opts.Settings = h.settings
	s := New(opts)
	s.owned = h
	return s, nil
}
func (s *Session) ConfigureExecutionAdmission(port api.ExecutionAdmission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admission = port
}
func (s *Session) ConfigureDispatchSource(annotate func(context.Context, api.WorkDispatchSource) (api.WorkDispatchSource, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatchSource = annotate
}
func (s *Session) OwnedRuntimeReady(ctx context.Context) error {
	s.mu.Lock()
	h, model := s.owned, s.execution.Model
	s.mu.Unlock()
	if h == nil {
		return errors.New("shared Caelis Host has no owned runtime proof")
	}
	return h.ready(ctx, model)
}
func (s *Session) FenceStop(ctx context.Context) error {
	s.mu.Lock()
	h := s.owned
	if h == nil {
		s.mu.Unlock()
		return errors.New("shared Caelis Host cannot be fenced by this APP")
	}
	s.connected = false
	if s.cancel != nil {
		s.cancel()
	}
	if s.streamCancel != nil {
		s.streamCancel()
	}
	s.mu.Unlock()
	return h.stop(ctx)
}

func (s *Session) ConfigureOwnedDeadline(ctx context.Context, epoch string, deadline time.Time) error {
	s.mu.Lock()
	h := s.owned
	s.mu.Unlock()
	if h == nil {
		return errors.New("shared Host cannot own a lease watchdog")
	}
	return h.process.ConfigureDeadline(ctx, epoch, deadline)
}
