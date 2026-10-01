package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type ownedLeaseProcess interface {
	PID() int
	Live() bool
	Renew(context.Context, string, time.Duration) error
	Stop(context.Context) error
}

func (s *Session) ConfigureOwnedDeadline(ctx context.Context, epoch string, deadline time.Time) error {
	s.mu.Lock()
	helper, p, prior := s.opts.WatchdogHelper, s.supervisor, s.ownedEpoch
	s.mu.Unlock()
	if helper == "" || epoch == "" || (prior != "" && prior != epoch) {
		return errors.New("managed runtime has no independent owned watchdog")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Until(deadline) <= time.Second || time.Until(deadline) > 45*time.Second {
		return errors.New("native watchdog deadline already exhausted")
	}
	if p != nil {
		if !p.Live() {
			return errors.New("independent owned watchdog stopped")
		}
		if err := p.Renew(ctx, epoch, time.Until(deadline)); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.ownedEpoch, s.ownedDeadline = epoch, deadline
	s.mu.Unlock()
	return nil
}
func (s *Session) OwnedRuntimeReady(ctx context.Context) error {
	s.mu.Lock()
	helper, p, deadline, verified := s.opts.WatchdogHelper, s.supervisor, s.ownedDeadline, s.supervisorVerified
	s.mu.Unlock()
	if !verified && p == nil {
		return errors.New("independent native watchdog has not passed its actual owned handshake")
	}
	if !filepath.IsAbs(helper) {
		return errors.New("managed runtime needs a pinned watchdog helper executable")
	}
	info, err := os.Stat(helper)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("managed native watchdog helper unavailable")
	}
	if p != nil && (!p.Live() || !time.Now().Before(deadline)) {
		return errors.New("independent native watchdog is not live")
	}
	return ctx.Err()
}

func (s *Session) VerifyOwnedSupervisor(ctx context.Context) error {
	s.mu.Lock()
	helper, binary, directory := s.opts.WatchdogHelper, s.opts.Binary, s.opts.Directory
	s.mu.Unlock()
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	status, err := ProbeSupervisedAuth(ctx, helper, binary, directory)
	if err != nil {
		return err
	}
	if status.RequiresOpenAIAuth && !status.AccountPresent {
		return errors.New("managed Codex needs target-side native authentication")
	}
	s.mu.Lock()
	s.supervisorVerified = true
	s.mu.Unlock()
	return nil
}
