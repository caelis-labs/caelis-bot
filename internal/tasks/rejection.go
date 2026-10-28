package tasks

import (
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func (m *Manager) ConfigureDiagnostics(log *diagnosticlog.Logger) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.diagnostics = log
	m.mu.Unlock()
}

func (m *Manager) startTaskID(in api.TaskStart) string {
	if in.Machine != "" {
		return "task-" + hash("remote", in.Machine, in.RequestID)
	}
	if _, ok := m.work.(api.WorkRouter); ok {
		return "task-" + hash("local", in.RequestID)
	}
	return "task-" + hash(m.provider, in.RequestID)
}

// Only a fresh, proven refusal is diagnostic evidence. A failed admission check
// for an already dispatched request must retain its original unknown receipt.
func (m *Manager) logStartRejection(in api.TaskStart, id string, task api.Task, err error) {
	var rejection *api.WorkMessageRejection
	if task.Outcome != "rejected" || !errors.As(err, &rejection) {
		return
	}
	m.mu.Lock()
	log := m.diagnostics
	m.mu.Unlock()
	reason := rejection.Code
	if reason == "" {
		reason = "unspecified"
	}
	workspace := in.Workspace
	if workspace == "" && in.Machine == "" {
		workspace = filepath.Join(m.root, id)
	}
	log.Write(diagnosticlog.Record{
		Level: "warn", Component: "tasks", Code: "start_rejected_before_dispatch",
		TaskID: id, RequestID: in.RequestID, Reason: reason, Workspace: workspace,
	})
}
