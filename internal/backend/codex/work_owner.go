package codex

import (
	"context"
	"errors"
	"strings"

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
	// If explicit recovery durably archived a private owner but preparing its
	// replacement failed, the host's endpoint cache may still name that old
	// socket. The original archive is proof to ignore only that stale hint.
	if s.binding.OwnerEndpoint == "" && len(s.binding.LostOwners) > 0 {
		old := s.binding.LostOwners[len(s.binding.LostOwners)-1]
		if old.Process.valid() && s.opts.Socket == strings.TrimPrefix(old.Endpoint, "unix://") {
			s.opts.Socket, s.opts.RequiredSocket = "", false
		}
	}
	s.resetProjection() // Restore task-owned subscriptions after loading the binding.
	return &WorkOwner{s}, nil
}

// NewRetainedWorkOwner opens only the Workers in an inactive local provider's
// existing binding. It never resumes or exposes that provider's resident chat.
func NewRetainedWorkOwner(opts SessionOptions) (*WorkOwner, error) {
	if opts.BotTools != nil {
		return nil, errors.New("workers cannot inherit Bot tools")
	}
	s := NewSession(opts)
	s.workerOnly = true
	s.resetProjection() // Restore task-owned subscriptions after loading the binding.
	return &WorkOwner{s}, s.loadErr
}
func (w *WorkOwner) Close(ctx context.Context) error { return w.engine.Close(ctx) }
func (w *WorkOwner) Snapshot() api.Snapshot {
	return api.Snapshot{Approvals: w.engine.Snapshot().Approvals}
}
func (w *WorkOwner) ApprovalURL(id string) (string, error)            { return w.engine.ApprovalURL(id) }
func (w *WorkOwner) Decide(ctx context.Context, d api.Decision) error { return w.engine.Decide(ctx, d) }
func (w *WorkOwner) PrepareDetachForUpdate(ctx context.Context) error {
	return w.engine.PrepareDetachForUpdate(ctx)
}
func (w *WorkOwner) DetachForUpdate(ctx context.Context) error {
	return w.engine.DetachForUpdate(ctx)
}
func (w *WorkOwner) WorkMessageRecorded(in api.TaskMessage) bool {
	return w.engine.WorkMessageRecorded(in)
}
func (w *WorkOwner) SetModel(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	return w.engine.ChangeWorkExecution(ctx, v, persist)
}

func (w *WorkOwner) Connect(ctx context.Context) error { return w.engine.Connect(ctx) }

// RecoverNewWork is an explicit host action. It prepares new work only after
// verifying the original private process has exited and preserving its tasks
// and uncertain receipts under the existing lost-owner archive.
func (w *WorkOwner) RecoverNewWork(ctx context.Context) error { return w.engine.recoverNewWork(ctx) }

// ConnectedForStop lets the local owner router reach StopWork's pre-lock
// cancellation without first waiting for Connect on the active Turn lock.
func (w *WorkOwner) ConnectedForStop() bool {
	w.engine.mu.Lock()
	defer w.engine.mu.Unlock()
	return w.engine.client != nil && w.engine.state.Connection == "ready"
}
func (w *WorkOwner) WorkAdmission(ctx context.Context) error { return w.engine.WorkAdmission(ctx) }
func (w *WorkOwner) WorkStates() []api.WorkState             { return w.engine.WorkStates() }
func (w *WorkOwner) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	return w.engine.StartWork(ctx, in)
}
func (w *WorkOwner) ReadWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.ReadWork(ctx, id)
}
func (w *WorkOwner) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	return w.engine.ReadWorkConversation(ctx, id)
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
// unavailable private socket must fail instead of starting a replacement;
// shared daemon recovery retains its original standard endpoint and data home.
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

func (w *WorkOwner) WorkCapabilities(ctx context.Context, id string) (api.WorkCapabilities, error) {
	return w.engine.WorkCapabilities(ctx, id)
}
func (w *WorkOwner) ObserveWork(ctx context.Context, id string) (api.WorkObservation, error) {
	return w.engine.ObserveWork(ctx, id)
}
func (w *WorkOwner) StopWorkRequest(ctx context.Context, in api.WorkStop) (api.Task, error) {
	return w.engine.StopWorkRequest(ctx, in)
}
func (w *WorkOwner) DecideWork(ctx context.Context, in api.WorkDecision) error {
	return w.engine.DecideWork(ctx, in)
}
func (w *WorkOwner) WorkApprovalURL(ctx context.Context, task, id string) (string, error) {
	return w.engine.WorkApprovalURL(ctx, task, id)
}
func (w *WorkOwner) ReadWorkOperation(ctx context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	return w.engine.ReadWorkOperation(ctx, q)
}
func (w *WorkOwner) ReadWorkFile(ctx context.Context, task, id string) (api.WorkFileContent, error) {
	return w.engine.ReadWorkFile(ctx, task, id)
}
