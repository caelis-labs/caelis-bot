package desktop

import (
	"path/filepath"
	"testing"
)

func TestDisplayPreferencesPersistAndRejectUnsupported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "display.json")
	s := &Service{}
	if err := s.configureDisplay(file); err != nil {
		t.Fatal(err)
	}
	events := 0
	s.displayChanged = func(DisplayState) { events++ }
	next, err := s.SaveDisplay("light", "default")
	if err != nil || next.Mode != "light" || events != 1 {
		t.Fatalf("save: %+v %v events=%d", next, err, events)
	}
	other := &Service{}
	if err := other.configureDisplay(file); err != nil || other.DisplayPreferences().Mode != "light" {
		t.Fatalf("restart: %v %+v", err, other.DisplayPreferences())
	}
	if _, err := s.SaveDisplay("dark", "unknown"); err == nil {
		t.Fatal("accepted unknown theme")
	}
	if s.DisplayPreferences() != next || events != 1 {
		t.Fatal("invalid save changed display")
	}
	s.displayFile = filepath.Join(file, "invalid")
	if _, err := s.SaveDisplay("dark", "default"); err == nil {
		t.Fatal("expected write failure")
	}
	if s.DisplayPreferences() != next || events != 1 {
		t.Fatal("failed write changed display")
	}
}
