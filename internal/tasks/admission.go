package tasks

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ConfigureExecutionAdmission is called before exposing task tools.
func (m *Manager) ConfigureExecutionAdmission(port api.ExecutionAdmission) {
	m.executionAdmission = port
}
