package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

// ConfigureDesktopControl binds the resident-only Desktop World provider.
func (r *Runtime) ConfigureDesktopControl(driver api.ApplicationTools) { r.desktopControl = driver }

func (r *Runtime) BeginDesktopTurn() {
	r.desktopLifecycle.Lock()
	defer r.desktopLifecycle.Unlock()
	r.stopDesktopTurn()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.desktopTurn = rand.Text()
	r.desktopQueries = nil
	r.desktopContext, r.desktopCancel = context.WithCancel(context.Background())
	if r.stopped {
		r.desktopCancel()
	}
}
func (r *Runtime) StopDesktopTurn() {
	r.desktopLifecycle.Lock()
	defer r.desktopLifecycle.Unlock()
	r.stopDesktopTurn()
}
func (r *Runtime) stopDesktopTurn() {
	r.mu.Lock()
	turn := r.desktopTurn
	if r.desktopCancel != nil {
		r.desktopCancel()
	}
	r.mu.Unlock()
	if driver, ok := r.desktopControl.(interface{ EndTurn(string) }); ok {
		driver.EndTurn(turn)
	}
}
func (r *Runtime) desktopCallContext(parent context.Context) (context.Context, context.CancelFunc) {
	r.mu.Lock()
	turn := r.desktopContext
	turnID := r.desktopTurn
	r.mu.Unlock()
	ctx, cancel := context.WithCancel(desktopcontrol.WithTurn(parent, turnID))
	if turn == nil || turn.Err() != nil {
		cancel()
		return ctx, cancel
	}
	stop := context.AfterFunc(turn, cancel)
	return ctx, func() { stop(); cancel() }
}

func (r *Runtime) requireDesktopImage(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	engine := r.engine
	r.mu.Unlock()
	provider, ok := engine.(api.ImageInputProvider)
	if !ok {
		return errors.New("selected model image support is unknown")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	capability, err := provider.ImageInput(ctx)
	if err != nil || capability.State != "supported" {
		return errors.New("select an image-capable model before requesting a screenshot")
	}
	return nil
}
