package caelis

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// WorkOwner exposes only shared native workers. Its empty management binding
// never receives a model turn, Bot tools, Notebook, skills or schedules.
// Mutations enter through a trusted host boundary (legacy private SSH helper or
// authenticated peer factory), never a caller-supplied native source assertion.
type WorkOwner struct{ engine *Session }

func NewWorkOwner(opts Options) (*WorkOwner, error) {
	s := New(opts)
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.state.WorkSourceIdentity != opts.WorkSourceIdentity {
		if s.state.WorkSourceIdentity != "" || len(s.state.Workers) != 0 {
			return nil, errors.New("original Worker source identity differs")
		}
		s.state.WorkSourceIdentity = opts.WorkSourceIdentity
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	if err := s.ConfigureBotTools(&api.ToolConnection{}); err != nil {
		return nil, err
	}
	return &WorkOwner{s}, nil
}

// Retained local Workers keep the exact application/session and credential
// binding. Observation cannot rebind the inactive Bot's tools or run its calls.
func NewRetainedWorkOwner(opts Options) (*WorkOwner, error) {
	w, err := NewWorkOwner(opts)
	if err != nil {
		return nil, err
	}
	w.engine.retainedWorkers = true
	return w, w.engine.loadErr
}
func (w *WorkOwner) Connect(ctx context.Context) error { return w.engine.Connect(ctx) }
func (w *WorkOwner) Close(ctx context.Context) error   { return w.engine.Close(ctx) }
func (w *WorkOwner) Snapshot() api.Snapshot {
	v := w.engine.Snapshot()
	return api.Snapshot{Connection: v.Connection, ConnectionIssue: v.ConnectionIssue, Approvals: v.Approvals}
}
func (w *WorkOwner) Decide(ctx context.Context, d api.Decision) error { return w.engine.Decide(ctx, d) }
func (w *WorkOwner) DetachForUpdate(ctx context.Context) error        { return w.engine.DetachForUpdate(ctx) }

type workOwnerKey struct{}

func (w *WorkOwner) ownedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, workOwnerKey{}, w.engine)
}
func (w *WorkOwner) WorkAdmission(ctx context.Context) error {
	if err := w.engine.WorkAdmission(w.ownedContext(ctx)); err != nil {
		return err
	}
	w.engine.mu.Lock()
	profile := w.engine.profile
	w.engine.mu.Unlock()
	_, err := w.engine.resolveWorkExecution(ctx, profile)
	return err
}
func (w *WorkOwner) WorkStates() []api.WorkState { return w.engine.WorkStates() }
func (w *WorkOwner) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	return w.engine.StartWork(w.ownedContext(ctx), in)
}
func (w *WorkOwner) ReadWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.ReadWork(ctx, id)
}
func (w *WorkOwner) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	return w.engine.ReadWorkConversation(ctx, id)
}
func (w *WorkOwner) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	return w.engine.SendWork(w.ownedContext(ctx), in)
}
func (w *WorkOwner) WorkMessageRecorded(in api.TaskMessage) bool {
	return w.engine.WorkMessageRecorded(in)
}
func (w *WorkOwner) StopWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.StopWork(ctx, id)
}
func (w *WorkOwner) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	return w.engine.WorkTerminal(ctx, id)
}
func (w *WorkOwner) SetModel(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	return w.engine.ChangeWorkExecution(ctx, v, persist)
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
func (w *WorkOwner) ReadWorkOperation(ctx context.Context, in api.WorkOperationQuery) (api.WorkOperation, error) {
	return w.engine.ReadWorkOperation(ctx, in)
}
func (w *WorkOwner) ReadWorkFile(ctx context.Context, task, id string) (api.WorkFileContent, error) {
	return w.engine.ReadWorkFile(ctx, task, id)
}
