package codex

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// WorkOwner is target-only assembly behind an authenticated SSH channel. It
// exposes no resident conversation, scheduler, skills or Bot tool connection.
// The originating app must check its native invocation before every mutation.
type WorkOwner struct{ engine *Session }

func NewWorkOwner(opts SessionOptions) (*WorkOwner, error) {
	if opts.BotTools != nil {
		return nil, errors.New("remote workers cannot inherit Bot tools")
	}
	opts.RequiredSocket = opts.Socket != ""
	s := NewSession(opts)
	if s.binding.ThreadID != "" || s.binding.Pending != nil || len(s.binding.Scheduled) > 0 {
		return nil, errors.New("resident bindings cannot become a worker owner")
	}
	s.workerOnly = true
	return &WorkOwner{s}, nil
}
func (w *WorkOwner) SetModel(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	return w.engine.ChangeWorkExecution(ctx, v, persist)
}

func (w *WorkOwner) Connect(ctx context.Context) error       { return w.engine.Connect(ctx) }
func (w *WorkOwner) WorkAdmission(ctx context.Context) error { return w.engine.WorkAdmission(ctx) }
func (w *WorkOwner) WorkStates() []api.WorkState             { return w.engine.WorkStates() }
func (w *WorkOwner) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	return w.engine.StartWork(ctx, in)
}
func (w *WorkOwner) ReadWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.ReadWork(ctx, id)
}
func (w *WorkOwner) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	return w.engine.SendWork(ctx, in)
}
func (w *WorkOwner) StopWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.StopWork(ctx, id)
}
func (w *WorkOwner) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	return w.engine.WorkTerminal(ctx, id)
}

// NativeEndpoint is recorded before admitting any task. Reconnecting to an
// unavailable original socket must fail instead of starting a replacement.
func (w *WorkOwner) NativeEndpoint() (string, error) {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return "", errors.New("native owner unavailable")
	}
	endpoint, ok := s.client.rpc.conn.(interface{ terminalEndpoint() string })
	if !ok || endpoint.terminalEndpoint() == "" {
		return "", errors.New("native endpoint unavailable")
	}
	return endpoint.terminalEndpoint(), nil
}

func (w *WorkOwner) Models(ctx context.Context) ([]api.ModelOption, error) {
	return w.engine.Models(ctx)
}

func (w *WorkOwner) RuntimeDefault(ctx context.Context) (api.WorkExecutionSettings, error) {
	return w.engine.RuntimeDefault(ctx)
}
