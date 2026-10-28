package desktop

import (
	"encoding/json"
	"errors"
	"os"
)

type taskShortcutDriver interface {
	registerTaskShortcut(Shortcut) error
	toggleTaskDock()
}

func defaultTaskShortcut() Shortcut {
	return Shortcut{Enabled: true, Key: "KeyT", Control: true, Shift: true}
}
func (s *Service) configureTaskShortcut(path string) {
	s.taskShortcutFile = path
	s.taskShortcut.Shortcut = defaultTaskShortcut()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var v Shortcut
	if err != nil || json.Unmarshal(b, &v) != nil || validateShortcut(v, s.LanguagePreferences().Locale) != nil {
		s.taskShortcut.Message = s.text("native.shortcutConfigUnreadable", nil)
		return
	}
	s.taskShortcut.Shortcut = v
}
func (s *Service) TaskShortcutSettings() ShortcutState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskShortcut
}
func (s *Service) SaveTaskShortcut(v Shortcut) (ShortcutState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateShortcut(v, s.LanguagePreferences().Locale); err != nil {
		return s.taskShortcut, err
	}
	return s.saveShortcutSlot(v, 1, s.taskShortcutFile)
}

func (s *Service) ToggleTaskDock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(taskShortcutDriver); ok && !s.stopped {
		d.toggleTaskDock()
	}
}
