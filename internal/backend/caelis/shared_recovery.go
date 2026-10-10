package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/caelisruntime"
)

func (s *Session) connectWithRecovery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("连接已停止")
	}
	if err := s.connect(ctx); err == nil {
		s.recoveryEvidence = false
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	} else {
		s.mu.Lock()
		bound, principal := s.state.StoreID != "", s.state.PrincipalID
		s.mu.Unlock()
		if !bound {
			return err
		}
		// Snapshot the original discovery BEFORE public status may clean a stale
		// record. Clean shutdown removes discovery; missing/invalid/foreign records
		// cannot authorize a cold start. A failed authorized start may retry after
		// cooldown even when status has already cleaned the stale record.
		d, _, discoveryErr := Discover(s.settings)
		allow := s.recoveryEvidence || discoveryErr == nil && d.PrincipalID == principal
		if recoveryErr := caelisruntime.Recover(ctx, s.settings.CLIPath, s.settings.CaelisStore, allow); recoveryErr != nil {
			if errors.Is(recoveryErr, caelisruntime.ErrServiceStartFailed) {
				s.recoveryEvidence = true
			}
			return recoveryErr
		}
		// The new discovery, handshake, Store/principal check and original
		// connection/operation journals remain authoritative for reconciliation.
		err = s.connect(ctx)
		if err == nil {
			s.recoveryEvidence = false
		} else if allow {
			s.recoveryEvidence = true
		}
		return err
	}
}
