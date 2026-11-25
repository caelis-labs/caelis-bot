package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
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
		if err != nil || string(data) != "{\"preference\":\"en\"}\n" {
			t.Fatalf("publication did not observe the saved preference: %q, %v", data, err)
		}
		events = append(events, state)
	}
	state, err := s.SaveLanguage("en")
	if err != nil || state.Locale != i18n.English || len(events) != 1 {
		t.Fatal(state, err, events)
	}
	stat, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && stat.Mode().Perm() != 0600 {
		t.Fatal(stat.Mode())
	}
	restarted := newService(&memoryStore{})
	if err := restarted.configureLanguage(file, []string{"zh"}); err != nil {
		t.Fatal(err)
	}
	if restarted.LanguagePreferences().Preference != "en" {
		t.Fatal("preference lost across restart")
	}
}

func TestLanguageFailedSavesDoNotPublish(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "settings")
	s := newService(&memoryStore{})
	if err := s.configureLanguage(filepath.Join(parent, "language.json"), []string{"en"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	initial := s.LanguagePreferences()
	s.languageChanged = func(state LanguageState) {
		t.Errorf("failed save published %+v", state)
	}
	if got, err := s.SaveLanguage("bogus"); err == nil || got != initial {
		t.Fatalf("invalid preference: got %+v, %v", got, err)
	}
	got, err := s.SaveLanguage("zh-CN")
	var failure *localstate.WriteError
	if got != initial || !errors.As(err, &failure) || failure.Stage != "directory" {
		t.Fatalf("blocked storage: got %+v, %v", got, err)
	}
	body, err := os.ReadFile(parent)
	if err != nil || string(body) != "existing" || s.LanguagePreferences() != initial {
		t.Fatalf("failed save changed file or state: %q, %v, %+v", body, err, s.LanguagePreferences())
	}
}

func TestLanguageConcurrentSavesStayOrdered(t *testing.T) {
	initial := LanguageState{Preference: "system", Locale: i18n.English, Revision: 1}
	first := LanguageState{Preference: "zh-CN", Locale: i18n.Chinese, Revision: 2}
	second := LanguageState{Preference: "en", Locale: i18n.English, Revision: 3}
	s := &Service{language: initial, systemLanguages: []string{"en"}}
	var steps []string
	var stepsMu sync.Mutex
	record := func(step string) {
		stepsMu.Lock()
		steps = append(steps, step)
		stepsMu.Unlock()
	}
	publishingFirst, finishFirstPublication := make(chan struct{}), make(chan struct{})
	assertSaveLocked := func(stage string) {
		// Verify the lock covers both boundaries regardless of when the
		// competing goroutine is scheduled.
		if s.languageSaveMu.TryLock() {
			s.languageSaveMu.Unlock()
			t.Errorf("save admission released during %s", stage)
		}
	}
	s.persistLanguage = func(preference string) error {
		assertSaveLocked("persistence")
		record("save " + preference)
		if preference == first.Preference {
			if got := s.LanguagePreferences(); got != initial {
				t.Errorf("while writing first preference: got %+v, want %+v", got, initial)
			}
		}
		return nil
	}
	s.languageChanged = func(state LanguageState) {
		assertSaveLocked("publication")
		if got := s.LanguagePreferences(); got != state {
			t.Errorf("publication is not readable: got %+v, want %+v", got, state)
		}
		if state.Preference == first.Preference {
			close(publishingFirst)
			<-finishFirstPublication
		}
		record("publish " + state.Preference)
	}
	save := func(want LanguageState) {
		if got, err := s.SaveLanguage(want.Preference); err != nil || got != want {
			t.Errorf("save %s: got %+v, %v, want %+v", want.Preference, got, err, want)
		}
	}
	var wg sync.WaitGroup
	firstReturned := make(chan struct{})
	wg.Go(func() {
		defer close(firstReturned)
		save(first)
	})
	select {
	case <-publishingFirst:
	case <-firstReturned:
		t.Fatal("first save returned without publishing")
	}
	startingSecond := make(chan struct{})
	wg.Go(func() {
		close(startingSecond)
		save(second)
	})
	<-startingSecond
	close(finishFirstPublication)
	wg.Wait()
	if want := []string{"save zh-CN", "publish zh-CN", "save en", "publish en"}; !slices.Equal(steps, want) {
		t.Fatalf("save/publication order: got %v, want %v", steps, want)
	}
	if got := s.LanguagePreferences(); got != second {
		t.Fatalf("final preference: got %+v, want %+v", got, second)
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
