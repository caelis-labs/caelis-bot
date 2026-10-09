package codex

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// CompleteToolTurn uses only the public app-server turn/interrupt operation.
// It targets the resident Turn observed when this Bot MCP request entered.
func (s *Session) CompleteToolTurn(ctx context.Context, invocation api.ToolInvocation) error {
	if invocation.Provider != "codex" || invocation.CallID == "" || invocation.Session == "" || invocation.Turn == "" {
		return errors.New("original Codex MCP invocation is unavailable")
	}
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.binding.RenewedBy == invocation.CallID {
		s.mu.Unlock()
		return nil
	}
	if s.binding.ThreadID != invocation.Session || s.lastTurn != invocation.Turn || s.client == nil {
		s.mu.Unlock()
		return errors.New("original Codex Turn changed before interruption")
	}
	status := s.runs[invocation.Turn]
	if status == "interrupted" {
		s.mu.Unlock()
		return nil
	}
	if terminal(status) || s.run != invocation.Turn {
		s.mu.Unlock()
		return errors.New("original Codex Turn completed before interruption")
	}
	c := s.client
	s.mu.Unlock()
	if err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": invocation.Session, "turnId": invocation.Turn}, nil); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		status, changed := s.runs[invocation.Turn], s.changed
		s.mu.Unlock()
		if status == "interrupted" {
			return nil
		}
		if terminal(status) {
			return errors.New("original Codex Turn ended without confirmed interruption")
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.Done():
			return errors.New("original Codex Turn interruption result is unknown")
		}
	}
}
