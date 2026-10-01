package workerwire

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The paired Worker stream carries no TTY/attach capability. Never return its
// target-host paths to a local shell or issue another task to emulate a terminal.
func (c *Client) WorkTerminal(ctx context.Context, _ string) (api.TerminalTarget, error) {
	if err := ctx.Err(); err != nil {
		return api.TerminalTarget{}, err
	}
	return api.TerminalTarget{}, api.ErrRemoteWorkTerminal
}
