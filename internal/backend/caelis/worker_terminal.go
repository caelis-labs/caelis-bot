package caelis

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// A Worker-only connection has neither the target user's Host credential nor a
// negotiated terminal attachment channel. Application credentials cannot stand
// in for that authority, even when its control endpoint is a local SSH tunnel.
func (c *WorkerClient) WorkTerminal(ctx context.Context, _ string) (api.TerminalTarget, error) {
	if err := ctx.Err(); err != nil {
		return api.TerminalTarget{}, err
	}
	return api.TerminalTarget{}, api.ErrRemoteWorkTerminal
}
