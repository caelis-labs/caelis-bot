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
func (s *Session) FenceStop(ctx context.Context) error {
	s.mu.Lock()
	if !s.opts.ForceOwned {
		s.mu.Unlock()
		return errors.New("runtime is not an isolated owned generation")
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
	if err := client.toolCleanupError(); err != nil {
		return errors.Join(freezeErr, forceErr, err)
	}
	return errors.Join(freezeErr, forceErr)
}
