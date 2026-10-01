package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Session) ConfigureExecutionAdmission(port api.ExecutionAdmission) { s.opts.Admission = port }

// FenceStop is the leased owner's hard process boundary. It bypasses queued
// graceful protocol operations and never targets a discovered shared server.
// Already dispatched external effects retain their unknown native receipts.
func (s *Session) FenceStop(ctx context.Context) error { return s.fenceOwned(ctx, true) }
func (s *Session) fenceOwned(ctx context.Context, isolated bool) error {
	s.mu.Lock()
	if isolated && !s.opts.ForceOwned {
		s.mu.Unlock()
		return errors.New("runtime is not an isolated owned generation")
	}
	if !isolated && (s.client == nil || s.client.rpc.stop == nil) {
		s.mu.Unlock()
		return errors.New("shared runtime cannot supply stopped ownership proof")
	}
	s.closing = true
	s.cancelLife()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	freezer, ok := client.rpc.conn.(interface{ freezeOwned() error })
	if !ok {
		return errors.New("owned runtime fencing port unavailable")
	}
	freezeErr := freezer.freezeOwned()
	client.captureTools()
	process, ok := client.rpc.conn.(interface{ forceKillOwned() error })
	if !ok {
		return errors.New("owned immediate termination port unavailable")
	}
	forceErr := process.forceKillOwned()
	client.Close()
	stopErr := errors.Join(freezeErr, forceErr, client.toolCleanupError())
	if stopErr == nil {
		// Native termination is already proven. Later application cleanup must
		// not send graceful background-clean RPCs to the deliberately dead host.
		// Pending/unknown operation state is retained for SafeIdle proof.
		s.mu.Lock()
		s.closed = true
		s.state.Connection = "offline"
		s.update()
		s.mu.Unlock()
	}
	return stopErr
}

// FenceOwnedForBootstrap is available only to an explicitly invoked native
// source export. Ownership comes from the retained launched-process handle,
// including the ordinary private attachable owner, never a settings flag.
func (s *Session) FenceOwnedForBootstrap(ctx context.Context) error { return s.fenceOwned(ctx, false) }
func (s *Session) OwnsLiveRuntime() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownsLiveRuntimeLocked()
}

// The caller holds s.mu. Ownership comes from retained native handles, never
// from installation, authentication, an endpoint or a configured settings flag.
func (s *Session) ownsLiveRuntimeLocked() bool {
	if !s.liveRuntimeLocked() || s.client.rpc.stop == nil {
		return false
	}
	_, freezes := s.client.rpc.conn.(interface{ freezeOwned() error })
	_, kills := s.client.rpc.conn.(interface{ forceKillOwned() error })
	return freezes && kills
}

func (s *Session) liveRuntimeLocked() bool {
	return !s.closed && !s.closing && s.client != nil && s.client.Err() == nil
}
