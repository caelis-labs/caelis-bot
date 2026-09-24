package desktop

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// LanguageState is a host-owned preference shared by every window. Revision
// fences a late initial read or save response against a newer native event.
type LanguageState struct {
	Preference string      `json:"preference"`
	Locale     i18n.Locale `json:"locale"`
	Revision   uint64      `json:"revision"`
}

func (s *Service) configureLanguage(file string, languages []string) error {
	s.languageFile = file
	s.systemLanguages = append([]string(nil), languages...)
	preference := "system"
	data, err := os.ReadFile(file)
	if err == nil {
		var stored struct {
			Preference string `json:"preference"`
		}
		err = json.Unmarshal(data, &stored)
		if err == nil && !i18n.ValidPreference(stored.Preference) {
			err = errors.New("invalid language preference")
		}
		if err == nil {
			preference = stored.Preference
		}
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	s.language = LanguageState{Preference: preference, Locale: i18n.Resolve(preference, languages), Revision: 1}
	return err
}

// LanguagePreferences reads the current preference without changing Runtime,
// conversation identity, model instructions or other desktop settings.
func (s *Service) LanguagePreferences() LanguageState {
	s.languageMu.Lock()
	defer s.languageMu.Unlock()
	return s.language
}

// SaveLanguage persists before notifying renderers and menus. A failed save
// changes neither their language nor the preference returned on the next read.
func (s *Service) SaveLanguage(preference string) (LanguageState, error) {
	s.languageSaveMu.Lock()
	defer s.languageSaveMu.Unlock()
	current := s.LanguagePreferences()
	if !i18n.ValidPreference(preference) {
		return current, errors.New("unsupported language preference")
	}
	if err := localstate.Write(s.languageFile, struct {
		Preference string `json:"preference"`
	}{preference}); err != nil {
		return current, err
	}
	next := LanguageState{Preference: preference, Locale: i18n.Resolve(preference, s.systemLanguages), Revision: current.Revision + 1}
	s.languageMu.Lock()
	s.language = next
	s.languageMu.Unlock()
	// Saves and publications are ordered, but native UI callbacks do not hold
	// the read mutex: other surface operations may read language while holding
	// their own locks. The callback must not recursively save a preference.
	if s.languageChanged != nil {
		s.languageChanged(next)
	}
	return next, nil
}

func (s *Service) text(key string, args map[string]any) string {
	return i18n.Text(s.LanguagePreferences().Locale, key, args)
}
