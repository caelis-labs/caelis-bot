package desktop

import (
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
)

type DisplayState struct {
	Mode     string `json:"mode"`
	Theme    string `json:"theme"`
	Revision uint64 `json:"revision"`
}

func validDisplay(v DisplayState) bool {
	return (v.Mode == "system" || v.Mode == "light" || v.Mode == "dark") && v.Theme == "default"
}
func (s *Service) configureDisplay(file string) error {
	s.displayFile = file
	s.display = DisplayState{Mode: "system", Theme: "default", Revision: 1}
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var v DisplayState
	if json.Unmarshal(b, &v) != nil || !validDisplay(v) {
		return errors.New("invalid display preferences")
	}
	v.Revision = 1
	s.display = v
	return nil
}
func (s *Service) DisplayPreferences() DisplayState {
	s.displayMu.Lock()
	defer s.displayMu.Unlock()
	return s.display
}
func (s *Service) SaveDisplay(mode, theme string) (DisplayState, error) {
	s.displaySaveMu.Lock()
	defer s.displaySaveMu.Unlock()
	old := s.DisplayPreferences()
	next := DisplayState{Mode: mode, Theme: theme, Revision: old.Revision + 1}
	if !validDisplay(next) {
		return old, errors.New("unsupported display preference")
	}
	if err := localstate.Write(s.displayFile, next); err != nil {
		return old, err
	}
	s.displayMu.Lock()
	s.display = next
	s.displayMu.Unlock()
	if s.displayChanged != nil {
		s.displayChanged(next)
	}
	return next, nil
}
