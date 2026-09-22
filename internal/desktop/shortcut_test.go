package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type shortcutFake struct {
	fakeDriver
	current  Shortcut
	reject   bool
	centered int
}

func (d *shortcutFake) registerShortcut(v Shortcut) error {
	if d.reject {
		return errors.New("conflict")
	}
	d.current = v
	return nil
}
func (d *shortcutFake) centeredPanel() { d.centered++; d.panelOpen = !d.panelOpen }
func (d *shortcutFake) panelReady(int) {}
func TestShortcutConflictPersistenceAndHiddenPet(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	path := filepath.Join(t.TempDir(), "shortcut.json")
	s.configureShortcut(path)
	d := &shortcutFake{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}
	s.start(d)
	v := defaultShortcut()
	v.Key = "KeyB"
	if _, err := s.SaveShortcut(v); err != nil {
		t.Fatal(err)
	}
	d.reject = true
	bad := v
	bad.Key = "KeyC"
	if _, err := s.SaveShortcut(bad); err == nil || s.ShortcutSettings().Shortcut != v || d.current != v {
		t.Fatal("conflict displaced previous shortcut")
	}
	d.reject = false
	// Persistence failure must roll the native registration back.
	s.shortcutFile = path + "/impossible"
	if _, err := s.SaveShortcut(bad); err == nil || d.current != v {
		t.Fatal("failed persistence changed active shortcut")
	}
	next := newService(&memoryStore{})
	next.configureShortcut(path)
	if next.ShortcutSettings().Shortcut != v {
		t.Fatal("shortcut did not survive restart")
	}
	if err := s.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	s.ToggleCenteredPanel()
	if d.centered != 1 || !d.panelOpen || d.placement.Visible {
		t.Fatal("global invocation depends on visible pet")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("preferences permissions", err)
	}
}
func TestShortcutRejectsPlainTypingKeys(t *testing.T) {
	for _, v := range []Shortcut{{Key: "Space", Enabled: true}, {Key: "KeyX", Shift: true}, {Key: "Escape", Control: true}} {
		if validateShortcut(v) == nil {
			t.Fatal("unsafe/unsupported shortcut", v)
		}
	}
	if validateShortcut(defaultShortcut()) != nil {
		t.Fatal("default invalid")
	}
}
