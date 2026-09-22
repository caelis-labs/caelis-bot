package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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
	centeredPanel()
	panelReady(int)
}

var shortcutKey = regexp.MustCompile(`^(Space|Key[A-Z]|Digit[0-9]|F([1-9]|1[0-2]))$`)

func defaultShortcut() Shortcut {
	return Shortcut{Enabled: true, Key: "Space", Control: true, Shift: true}
}
func validateShortcut(v Shortcut) error {
	if !shortcutKey.MatchString(v.Key) || (!v.Control && !v.Alt && !v.Meta) {
		return errors.New("请选择 Control、Option / Alt 或 Command / Meta 加空格、字母、数字或 F1–F12")
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
	if err != nil || json.Unmarshal(b, &v) != nil || validateShortcut(v) != nil {
		s.shortcut.Message = "快捷键配置无法读取，本次使用默认快捷键"
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
	if err := validateShortcut(v); err != nil {
		return s.shortcut, err
	}
	d, ok := s.native.(shortcutDriver)
	if !ok || s.stopped {
		return s.shortcut, errors.New("快捷键暂不可用")
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
			s.shortcut.Message = "保存失败，旧快捷键也未能恢复，请重新设置"
		}
		return s.shortcut, errors.New("快捷键未能保存，请重试")
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
func (s *Service) ToggleCenteredPanel() {
	// Opening a composer never approves, submits, or cancels anything.
	s.CloseHistory()
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(shortcutDriver); ok && !s.stopped {
		d.centeredPanel()
	}
}
func (s *Service) PanelReady(activation int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(shortcutDriver); ok && !s.stopped {
		d.panelReady(activation)
	}
}
