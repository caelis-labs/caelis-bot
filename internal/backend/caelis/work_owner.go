package caelis

import (
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

// WorkOwner exposes only shared native workers. Its empty management binding
// never receives a model turn, Bot tools, Notebook, skills or schedules.
// Mutations enter only through the target's private SSH-owned helper.
type WorkOwner struct{ engine *Session }
type noTools struct{}

func (noTools) Definitions() []api.ToolDefinition { return nil }
func (noTools) CallTool(context.Context, string, json.RawMessage) api.ToolResult {
	return api.ToolResult{IsError: true}
}
func NewWorkOwner(opts Options) (*WorkOwner, error) {
	s := New(opts)
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: noTools{}}); err != nil {
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
func (w *WorkOwner) Connect(ctx context.Context) error         { return w.engine.Connect(ctx) }
func (w *WorkOwner) Close(ctx context.Context) error           { return w.engine.Close(ctx) }
func (w *WorkOwner) DetachForUpdate(ctx context.Context) error { return w.engine.DetachForUpdate(ctx) }
func (w *WorkOwner) source(ctx context.Context, request string) context.Context {
	s := w.engine
	s.mu.Lock()
	defer s.mu.Unlock()
	call := wire.ApplicationCall{SessionId: s.state.Session.SessionId, ApplicationId: s.state.Connection.ApplicationId, ConnectionId: s.state.Connection.ConnectionId, PrincipalId: s.state.PrincipalID, Source: wire.ApplicationSource{Kind: "user", OperationId: request}}
	return context.WithValue(ctx, invocationKey{}, call)
}
func (w *WorkOwner) WorkAdmission(ctx context.Context) error {
	if err := w.engine.WorkAdmission(w.source(ctx, "ssh-native-user-request")); err != nil {
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
	return w.engine.StartWork(w.source(ctx, in.RequestID), in)
}
func (w *WorkOwner) ReadWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.ReadWork(ctx, id)
}
func (w *WorkOwner) RetireWork(ctx context.Context, id string) (api.Task, error) {
	return w.engine.RetireWork(ctx, id)
}
func (w *WorkOwner) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	return w.engine.SendWork(w.source(ctx, in.RequestID), in)
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
