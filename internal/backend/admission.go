package backend

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// ConfigureExecutionAdmission is called before connecting or publishing the service.
func (s *Service) ConfigureExecutionAdmission(port api.ExecutionAdmission) {
	s.executionAdmission = port
}
