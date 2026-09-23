package caelis

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The desktop now requires generic application execution. Keep its surfaces
// accessible while that external contract is under development; never silently
// activate legacy Bot Mode, adopt its tasks, or dispatch through another runtime.
var errApplicationProtocol = errors.New("Caelis 通用应用执行接口尚未接入；原有数据保留，请稍后更新或选择 Codex")

// ApplicationAvailability is the desktop admission gate until the replacement
// public protocol is implemented. It performs no legacy Bot mutations.
func ApplicationAvailability() error { return errApplicationProtocol }

func (s *Session) ConfigureBotTools(c *api.ToolConnection) error {
	if !s.applicationOwned || c == nil || c.Command == "" {
		return errApplicationProtocol
	}
	// This only validates host assembly. Connect remains explicitly unavailable;
	// no legacy endpoint receives the tool connection or its credentials.
	return nil
}
func (*Session) WorkAdmission(context.Context) error { return errApplicationProtocol }
func (*Session) WorkStates() []api.WorkState         { return nil }
func (*Session) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	return api.Task{}, errApplicationProtocol
}
func (*Session) ReadWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errApplicationProtocol
}
func (*Session) SendWork(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, errApplicationProtocol
}
func (*Session) StopWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errApplicationProtocol
}
func (*Session) SubmitReport(_ context.Context, in api.Submission) (api.Receipt, error) {
	return api.Receipt{ID: in.ID, Outcome: "rejected"}, errApplicationProtocol
}

var _ api.WorkRuntime = (*Session)(nil)
var _ api.ReportSubmitter = (*Session)(nil)
