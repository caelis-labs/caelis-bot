package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

type Preferences struct {
	MaxRunning    int    `json:"maxRunning"`
	Terminal      string `json:"terminal"`
	Revision      uint64 `json:"revision"`
	CustomCommand string `json:"customCommand"`
}
type PreferencesStore struct {
	mu    sync.Mutex
	path  string
	value Preferences
}

func OpenPreferences(path string) (*PreferencesStore, error) {
	s := &PreferencesStore{path: path, value: Preferences{MaxRunning: 3, Terminal: "system", Revision: 1}}
	b, err := os.ReadFile(path)
	if err == nil {
		if json.Unmarshal(b, &s.value) != nil || !validPreferences(s.value) || s.value.Revision == 0 {
			return nil, errors.New("invalid task preferences")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}
func validPreferences(p Preferences) bool {
	if p.MaxRunning < 1 || len(p.CustomCommand) > 4096 {
		return false
	}
	if p.Terminal == "custom" {
		_, err := taskterminal.CustomArgs(p.CustomCommand, "/private/terminal.command")
		return err == nil
	}
	return p.Terminal == "system" || p.Terminal == "terminal" || p.Terminal == "iterm2" || p.Terminal == "ghostty"
}
func (s *PreferencesStore) Snapshot() Preferences { s.mu.Lock(); defer s.mu.Unlock(); return s.value }
func (s *PreferencesStore) Save(p Preferences) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validPreferences(p) {
		return s.value, errors.New("invalid task preferences")
	}
	if p.Revision != s.value.Revision {
		return s.value, errors.New("task preferences changed; reload before saving")
	}
	p.Revision++
	if err := localstate.Write(s.path, p); err != nil {
		return s.value, err
	}
	s.value = p
	return p, nil
}
