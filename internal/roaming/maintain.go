package roaming

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// Maintain renews the installed generation, using request-start deadlines for
// every response. A failed confirmation immediately revokes autonomous work;
// no timer, wake or retry can activate a replacement generation implicitly.
func (g *Guard) Maintain(ctx context.Context, coordinator nodeplane.Coordinator) error {
	return g.maintain(ctx, coordinator, nodeplane.DefaultHeartbeatInterval)
}
func (g *Guard) maintain(ctx context.Context, coordinator nodeplane.Coordinator, interval time.Duration) error {
	if coordinator == nil {
		return errors.New("node coordinator is unavailable")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer g.Revoke()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.stoppedDone:
			return ErrFenced
		case <-ticker.C:
			lease, active := g.Lease()
			if !active {
				return ErrFenced
			}
			started := time.Now()
			requestCtx, cancel := context.WithTimeout(ctx, nodeplane.DefaultHeartbeatInterval)
			renewed, err := coordinator.Heartbeat(requestCtx, lease)
			cancel()
			if err != nil {
				return err
			}
			if err = g.Install(renewed, started); err != nil {
				return err
			}
		}
	}
}

// Lease returns host-only live authority; expired state is never usable proof.
func (g *Guard) Lease() (nodeplane.Lease, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lease, g.active && g.live(g.now())
}
