package app

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/care"
)

// PublishCareEvent accepts data only from in-process host adapters declared in
// Host.CareSources. It is not a renderer, HTTP, MCP or shell event-ingress API.
func (a *Application) PublishCareEvent(ctx context.Context, event care.Event) error {
	a.mu.Lock()
	r, ready := a.companion, a.started && !a.closed
	a.mu.Unlock()
	if !ready || r == nil {
		return errors.New("Bot is not running")
	}
	return r.PublishCareEvent(ctx, event)
}
