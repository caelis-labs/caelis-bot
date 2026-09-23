package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type shortcutFake struct {
	fakeDriver
	current Shortcut
	reject  bool
}

func (d *shortcutFake) registerShortcut(v Shortcut) error {
	if d.reject {
		return errors.New("conflict")
	}
	d.current = v
	return nil
}
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
	opened := 0
	s.openHistory = func() {
		if s.prepareWindowRecall() {
			opened++
		}
	}
	s.OpenHistory()
	s.OpenHistory() // Repeated shortcut invocation recalls chat; it never toggles closed.
	if opened != 2 || d.panelOpen || d.placement.Visible {
		t.Fatal("global invocation must open chat without showing the pet or quick input")
	}
	s.shutdown()
	s.OpenHistory()
	if opened != 2 {
		t.Fatal("late shortcut reopened chat after shutdown")
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

func TestChatShortcutTogglesNativeVisibilityWithoutOwningWork(t *testing.T) {
	s, d, store := setup()
	if err := s.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	visible, canHide := false, false
	opens, closes := 0, 0
	s.historyCanHide = func() bool { return visible && canHide }
	s.openHistory = func() { visible, canHide = true, true; opens++ }
	s.closeHistory = func() { visible = false; closes++ }
	s.ToggleHistory()
	if !visible || opens != 1 || closes != 0 {
		t.Fatal("shortcut did not open hidden chat")
	}
	s.ToggleHistory()
	if visible || opens != 1 || closes != 1 {
		t.Fatal("repeated shortcut did not hide chat")
	}
	s.ToggleHistory()
	// Native close must be observed, not a remembered toggle bit.
	s.CloseHistory()
	s.ToggleHistory()
	if !visible || opens != 3 || closes != 2 {
		t.Fatal("shortcut used stale state after native close")
	}
	// A minimised window or attached sheet requests recall instead of hide.
	canHide = false
	s.ToggleHistory()
	if !visible || opens != 4 || closes != 2 {
		t.Fatal("shortcut hid a minimised window or native sheet")
	}
	// Double-click/menu open remains idempotent, independent of shortcut policy.
	s.OpenHistory()
	if !visible || opens != 5 || d.panelOpen || d.placement.Visible || d.stopped || store.saves != 1 {
		t.Fatal("chat toggle changed another surface or app lifetime")
	}
	s.shutdown()
	s.ToggleHistory()
	if opens != 5 || closes != 2 {
		t.Fatal("shortcut acted after shutdown")
	}
}
