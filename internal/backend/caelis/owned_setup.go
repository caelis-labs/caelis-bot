package caelis

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

const ownedSetupLifetime = 10 * time.Minute

var ErrOwnedSetupClosed = errors.New("owned Caelis setup is closed")

// OwnedSetup is native-only ownership of one bounded setup foreground. It
// carries no Bot/Worker activation, application grant, or model authority.
// Callers retain it across their explicit wizard actions and close it on finish.
type OwnedSetup struct {
	host     *ownedHost
	life     context.Context
	cancel   context.CancelFunc
	deadline time.Time
	epoch    string
	mu       sync.Mutex
	closed   bool
	cause    error
	stopOnce sync.Once
	stopErr  error
	done     chan struct{}
}

// BeginOwnedSetup starts an already marked private Store for explicit human
// connection setup. It checks only public Host initialization, so an empty Store
// without model authentication is valid. PrepareOwnedStore belongs to the
// caller's separately authorized operation; this primitive never marks/adopts a
// Store. Its lifetime ends on ctx cancellation, Close, or ten minutes, with an
// independent owned watchdog deadline as well as in-process cleanup.
func BeginOwnedSetup(ctx context.Context, opts OwnedHostOptions) (*OwnedSetup, error) {
	if !codex.OwnedRuntimeSupported() {
		return nil, codex.ErrOwnedRuntimeUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if eligible, reason := ProbeOwnedStore(opts.NodeID, opts.Store); !eligible {
		return nil, readinessFailure(reason, errors.New("setup requires an existing exact private owned store"))
	}
	deadline := time.Now().Add(ownedSetupLifetime)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	life, cancel := context.WithDeadline(ctx, deadline)
	host, err := startOwnedHostWithStore(life, opts, true)
	if err != nil {
		cancel()
		return nil, readinessFailure("owned-setup-host-unavailable", err)
	}
	setup := &OwnedSetup{host: host, life: life, cancel: cancel, deadline: deadline, epoch: "setup-" + rand.Text(), done: make(chan struct{})}
	if err = setup.renew(life); err != nil {
		setup.fail(err)
		stop, c := context.WithTimeout(context.Background(), 4*time.Second)
		defer c()
		return nil, errors.Join(err, setup.Close(stop))
	}
	if err = setup.Check(life); err != nil {
		stop, c := context.WithTimeout(context.Background(), 4*time.Second)
		defer c()
		return nil, errors.Join(err, setup.Close(stop))
	}
	go setup.maintain()
	return setup, nil
}

// SetupSettings is a native SDK connection input, never a serialized response.
// It includes no Host token; the native adapter reads that target's authority.
func (s *OwnedSetup) SetupSettings(ctx context.Context) (api.RuntimeSettings, error) {
	if err := s.Check(ctx); err != nil {
		return api.RuntimeSettings{}, err
	}
	return s.host.settings, nil
}

// Check prevents stale wizard actions from using a stopped or replaced Host.
func (s *OwnedSetup) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	closed, cause := s.closed, s.cause
	s.mu.Unlock()
	if closed {
		return errors.Join(ErrOwnedSetupClosed, cause)
	}
	if err := s.life.Err(); err != nil {
		s.fail(err)
		return errors.Join(ErrOwnedSetupClosed, err)
	}
	if err := s.host.check(ctx); err != nil {
		failure := readinessFailure("owned-setup-host-unavailable", err)
		s.fail(failure)
		return failure
	}
	s.mu.Lock()
	closed, cause = s.closed, s.cause
	s.mu.Unlock()
	if closed || s.life.Err() != nil {
		return errors.Join(ErrOwnedSetupClosed, cause, s.life.Err())
	}
	return nil
}

// Close confirms the exact owned root and retained tools are stopped. Repeated
// calls return the same proof or original cleanup error; no new Host is started.
func (s *OwnedSetup) Close(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
		stop, finish := context.WithTimeout(ctx, 4*time.Second)
		defer finish()
		err := s.host.stop(stop)
		s.mu.Lock()
		if err != nil {
			s.stopErr = readinessFailure("owned-setup-stop-unconfirmed", err)
		}
		s.mu.Unlock()
		close(s.done)
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopErr
}

// Done closes after automatic or explicit cleanup finishes, including when
// cleanup returned an unknown result. Close retrieves that retained error.
func (s *OwnedSetup) Done() <-chan struct{} { return s.done }

func (s *OwnedSetup) fail(err error) {
	s.mu.Lock()
	if s.cause == nil {
		s.cause = err
	}
	s.mu.Unlock()
	s.cancel()
}
func (s *OwnedSetup) renew(ctx context.Context) error {
	deadline := time.Now().Add(40 * time.Second)
	if deadline.After(s.deadline) {
		deadline = s.deadline
	}
	return s.host.process.ConfigureDeadline(ctx, s.epoch, deadline)
}
func (s *OwnedSetup) maintain() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = s.Close(stop)
	}()
	for {
		select {
		case <-s.life.Done():
			s.fail(s.life.Err())
			return
		case <-ticker.C:
			if err := s.Check(s.life); err != nil {
				s.fail(err)
				return
			}
			renewal, cancel := context.WithTimeout(s.life, 3*time.Second)
			err := s.renew(renewal)
			cancel()
			if err != nil {
				s.fail(err)
				return
			}
		}
	}
}
