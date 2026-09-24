package desktop

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
)

func TestLanguagePersistenceAndPublication(t *testing.T) {
	file := filepath.Join(t.TempDir(), "language.json")
	s := newService(&memoryStore{})
	if err := s.configureLanguage(file, []string{"zh-Hans"}); err != nil {
		t.Fatal(err)
	}
	if state := s.LanguagePreferences(); state.Preference != "system" || state.Locale != i18n.Chinese {
		t.Fatal(state)
	}
	var events []LanguageState
	s.languageChanged = func(state LanguageState) {
		if s.LanguagePreferences() != state {
			t.Fatal("published state is not readable")
		}
		data, err := os.ReadFile(file)
		if err != nil || len(data) == 0 {
			t.Fatal("publish before persistence")
		}
		events = append(events, state)
	}
	state, err := s.SaveLanguage("en")
	if err != nil || state.Locale != i18n.English || len(events) != 1 {
		t.Fatal(state, err, events)
	}
	stat, _ := os.Stat(file)
	if stat.Mode().Perm() != 0600 {
		t.Fatal(stat.Mode())
	}
	restarted := newService(&memoryStore{})
	if err := restarted.configureLanguage(file, []string{"zh"}); err != nil {
		t.Fatal(err)
	}
	if restarted.LanguagePreferences().Preference != "en" {
		t.Fatal("preference lost across restart")
	}
	if _, err := s.SaveLanguage("bogus"); err == nil {
		t.Fatal("accepted unsupported preference")
	}
	s.languageFile = filepath.Join(file, "blocked.json")
	if _, err := s.SaveLanguage("zh-CN"); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if s.LanguagePreferences() != state || len(events) != 1 {
		t.Fatal("failed save changed state")
	}
}
func TestLanguageConcurrentSavesStayOrdered(t *testing.T) {
	s := newService(&memoryStore{})
	if err := s.configureLanguage(filepath.Join(t.TempDir(), "language.json"), []string{"en"}); err != nil {
		t.Fatal(err)
	}
	var last uint64 = 1
	s.languageChanged = func(state LanguageState) {
		if state.Revision != last+1 {
			t.Error("out-of-order publication")
		}
		last = state.Revision
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			if _, err := s.SaveLanguage("zh-CN"); err != nil {
				t.Error(err)
			}
			_ = s.LanguagePreferences()
		})
	}
	wg.Wait()
	if last != 9 {
		t.Fatal(last)
	}
}
func TestCorruptLanguageUsesSystemWithoutRewritingData(t *testing.T) {
	file := filepath.Join(t.TempDir(), "language.json")
	_ = os.WriteFile(file, []byte("broken"), 0600)
	s := newService(&memoryStore{})
	if err := s.configureLanguage(file, []string{"en"}); err == nil {
		t.Fatal("missing diagnostic")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "broken" || s.LanguagePreferences().Preference != "system" {
		t.Fatal("corrupt file silently rewritten")
	}
}
