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
	d, ok := s.native.(taskShortcutDriver)
	if !ok || s.stopped {
		return s.taskShortcut, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	old := s.taskShortcut.Shortcut
	if s.captureShortcutConflict(v, 1) {
		return s.taskShortcut, errors.New(s.text("native.shortcutConflict", nil))
	}
	if err := d.registerTaskShortcut(v); err != nil {
		return s.taskShortcut, err
	}
	if err := saveShortcut(s.taskShortcutFile, v); err != nil {
		if rollback := d.registerTaskShortcut(old); rollback != nil {
			disabled := v
			disabled.Enabled = false
			_ = d.registerTaskShortcut(disabled)
			s.taskShortcut.Registered = false
			s.taskShortcut.Message = s.text("native.shortcutRollbackFailed", nil)
		}
		return s.taskShortcut, errors.New(s.text("native.shortcutSaveFailed", nil))
	}
	s.taskShortcut = ShortcutState{Shortcut: v, Registered: v.Enabled}
	return s.taskShortcut, nil
}

func (s *Service) ToggleTaskDock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(taskShortcutDriver); ok && !s.stopped {
		d.toggleTaskDock()
	}
}
