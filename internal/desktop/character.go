package desktop

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

func characterActivity(v api.Snapshot) string {
	for _, approval := range v.Approvals {
		if approval.Status != "resolved" {
			return "waiting"
		}
	}
	if v.CanInterrupt {
		return "working"
	}
	return "idle"
}

// Facts flow one way from the backend observer into character presentation.
// No work identifiers, message text, or execution commands enter the renderer.
func (s *Service) observeCharacter(v api.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := characterActivity(v)
	if s.stopped || value == s.characterActivity {
		return
	}
	s.characterActivity = value
	if d, ok := s.native.(activityDriver); ok {
		d.activity(value)
	}
}
func (s *Service) CharacterActivity() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.characterActivity == "" {
		return "idle"
	}
	return s.characterActivity
}
