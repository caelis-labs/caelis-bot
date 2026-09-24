package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

// Physical key codes and platform-neutral modifiers; native drivers own mapping.
type Shortcut struct {
	Enabled bool   `json:"enabled"`
	Key     string `json:"key"`
	Control bool   `json:"control"`
	Alt     bool   `json:"alt"`
	Shift   bool   `json:"shift"`
	Meta    bool   `json:"meta"`
}
type ShortcutState struct {
	Shortcut   Shortcut `json:"shortcut"`
	Registered bool     `json:"registered"`
	Message    string   `json:"message"`
}
type shortcutDriver interface {
	registerShortcut(Shortcut) error
	panelReady(int)
}

var shortcutKey = regexp.MustCompile(`^(Space|Key[A-Z]|Digit[0-9]|F([1-9]|1[0-2]))$`)

func defaultShortcut() Shortcut {
	return Shortcut{Enabled: true, Key: "Space", Control: true, Shift: true}
}
func validateShortcut(v Shortcut, locale ...i18n.Locale) error {
	loc := i18n.DefaultLocale
	if len(locale) > 0 {
		loc = locale[0]
	}
	if !shortcutKey.MatchString(v.Key) || (!v.Control && !v.Alt && !v.Meta) {
		return errors.New(i18n.Text(loc, "native.shortcutInvalid", nil))
	}
	return nil
}
func (s *Service) configureShortcut(path string) {
	s.shortcutFile = path
	s.shortcut.Shortcut = defaultShortcut()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var v Shortcut
	if err != nil || json.Unmarshal(b, &v) != nil || validateShortcut(v, s.LanguagePreferences().Locale) != nil {
		s.shortcut.Message = s.text("native.shortcutConfigUnreadable", nil)
		return
	}
	s.shortcut.Shortcut = v
}
func (s *Service) ShortcutSettings() ShortcutState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shortcut
}
func (s *Service) SaveShortcut(v Shortcut) (ShortcutState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateShortcut(v, s.LanguagePreferences().Locale); err != nil {
		return s.shortcut, err
	}
	d, ok := s.native.(shortcutDriver)
	if !ok || s.stopped {
		return s.shortcut, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	old := s.shortcut.Shortcut
	if err := d.registerShortcut(v); err != nil {
		return s.shortcut, err
	}
	if err := saveShortcut(s.shortcutFile, v); err != nil {
		if rollback := d.registerShortcut(old); rollback != nil {
			disabled := v
			disabled.Enabled = false
			_ = d.registerShortcut(disabled)
			s.shortcut.Registered = false
			s.shortcut.Message = s.text("native.shortcutRollbackFailed", nil)
		}
		return s.shortcut, errors.New(s.text("native.shortcutSaveFailed", nil))
	}
	s.shortcut = ShortcutState{Shortcut: v, Registered: v.Enabled}
	return s.shortcut, nil
}
func saveShortcut(path string, v Shortcut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".shortcut-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(v)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err == nil {
		err = closed
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}
func (s *Service) PanelReady(activation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(shortcutDriver); ok && !s.stopped {
		d.panelReady(activation)
	}
}
