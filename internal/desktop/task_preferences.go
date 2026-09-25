package desktop

import (
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

func (s *Service) TaskPreferences() (tasks.Preferences, error) {
	if s.taskPreferences == nil {
		return tasks.Preferences{}, errors.New(s.text("native.taskPreferencesUnavailable", nil))
	}
	p := s.taskPreferences()
	if taskterminal.BundleID(p.Terminal) != "" && !s.terminalAvailable(p.Terminal) {
		p.Terminal = "system"
		if s.saveTaskPreferences == nil {
			return p, errors.New(s.text("native.taskPreferencesUnavailable", nil))
		}
		return s.saveTaskPreferences(p)
	}
	return p, nil
}
func (s *Service) SaveTaskPreferences(p tasks.Preferences) (tasks.Preferences, error) {
	if s.saveTaskPreferences == nil {
		return p, errors.New(s.text("native.taskPreferencesUnavailable", nil))
	}
	if taskterminal.BundleID(p.Terminal) != "" && !s.terminalAvailable(p.Terminal) {
		p.Terminal = "system"
	}
	return s.saveTaskPreferences(p)
}
func (s *Service) terminalAvailable(id string) bool {
	for _, choice := range s.TerminalChoices() {
		if choice.ID == id && choice.Available {
			return true
		}
	}
	return false
}
func (s *Service) TerminalChoices() []taskterminal.Choice {
	if s.terminalChoices == nil {
		return []taskterminal.Choice{{ID: "system", Available: true}}
	}
	return s.terminalChoices()
}
func (s *Service) unpinTask(id string) error {
	s.mu.Lock()
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if !ready || s.removeTaskPin == nil {
		return errors.New(s.text("native.taskPreferencesUnavailable", nil))
	}
	if err := s.removeTaskPin(id); err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if d, ok := s.native.(taskDriver); ok && !s.stopped {
			d.taskFailure(s.text("native.taskUnpinFailed", nil))
		}
		return err
	}
	return nil
}
