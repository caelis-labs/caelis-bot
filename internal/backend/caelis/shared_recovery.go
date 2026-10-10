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
		return nil
	} else if ctx.Err() != nil {
		return ctx.Err()
	} else {
		s.mu.Lock()
		bound := s.state.StoreID != "" && s.state.PrincipalID != "" &&
			s.state.StoreDirectory != "" && s.state.StoreDirectory == s.settings.CaelisStore
		s.mu.Unlock()
		if !bound {
			return err
		}
		// The saved Store and principal select the Host. Native status determines
		// whether it is stopped; handshake and credential failures while running
		// never authorize a restart.
		if recoveryErr := caelisruntime.Recover(ctx, s.settings.CLIPath, s.settings.CaelisStore); recoveryErr != nil {
			return recoveryErr
		}
		// The new discovery, handshake, Store/principal check and original
		// connection/operation journals remain authoritative for reconciliation.
		return s.connect(ctx)
	}
}
