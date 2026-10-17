package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

// Serializes submission with update preparation. Pausing does not close clients.
type windowsTerminalGate struct {
	mu     sync.Mutex
	paused bool
}

func (g *windowsTerminalGate) launch(ctx context.Context, submit func() (taskterminal.Window, error)) (taskterminal.Window, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused || ctx.Err() != nil {
		return nil, taskterminal.NotLaunched(errors.New("terminal launch paused for update"))
	}
	return submit()
}

func (g *windowsTerminalGate) pause() func() {
	g.mu.Lock()
	g.paused = true
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		g.paused = false
		g.mu.Unlock()
	}
}
