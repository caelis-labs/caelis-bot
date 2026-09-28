package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
)

// ConfigureDesktop is native-only; call before Serve/Start. A nil observer keeps
// the experiment absent. No observation callback is ever passed to a worker.
func (r *Runtime) ConfigureDesktop(observer desktopcontrol.Observer) { r.desktop = observer }

func (r *Runtime) ConfigureDesktopControl(driver api.ApplicationTools) {
	r.desktopControl = driver
	r.BeginDesktopTurn()
}

// Desktop input is scoped to the resident turn, including calls transported by
// MCP whose connection does not carry the runtime's cancellation context.
func (r *Runtime) BeginDesktopTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.desktopCancel != nil {
		r.desktopCancel()
	}
	r.desktopTurn = rand.Text()
	r.desktopContext, r.desktopCancel = context.WithCancel(context.Background())
	if r.stopped {
		r.desktopCancel()
	}
}
func (r *Runtime) StopDesktopTurn() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.desktopCancel != nil {
		r.desktopCancel()
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

func (r *Runtime) observeDesktop(ctx context.Context, args json.RawMessage) api.ToolResult {
	var input map[string]json.RawMessage
	if json.Unmarshal(args, &input) != nil || input == nil || len(input) != 0 {
		return result(nil, errors.New("desktop observation takes an empty object"))
	}
	if err := r.requireDesktopImage(ctx); err != nil {
		return result(nil, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	frame, err := r.desktop(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return result(nil, err)
	}
	out, err := desktopcontrol.Result(frame)
	if err != nil {
		return result(nil, err)
	}
	return out
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
