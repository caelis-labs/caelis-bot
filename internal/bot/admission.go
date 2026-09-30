package bot

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ConfigureExecutionAdmission installs host authority before Start or serving tools.
func (r *Runtime) ConfigureExecutionAdmission(port api.ExecutionAdmission) {
	r.executionAdmission = port
}
