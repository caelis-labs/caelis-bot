package desktop

import (
	"errors"
	"path/filepath"
	"testing"
)

type taskShortcutFake struct {
	shortcutFake
	tasks       Shortcut
	rejectTasks bool
	toggles     int
}

func (d *taskShortcutFake) registerTaskShortcut(v Shortcut) error {
	if d.rejectTasks {
		return errors.New("conflict")
	}
	d.tasks = v
	return nil
}
func (d *taskShortcutFake) toggleTaskDock() { d.toggles++ }
func TestTaskShortcutPersistsIndependentlyAndRollsBack(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	path := filepath.Join(t.TempDir(), "task-shortcut.json")
	s.configureTaskShortcut(path)
	s.configureShortcut(filepath.Join(t.TempDir(), "chat.json"))
	d := &taskShortcutFake{shortcutFake: shortcutFake{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}}
	s.start(d)
	v := defaultTaskShortcut()
	v.Key = "KeyW"
	if _, err := s.SaveTaskShortcut(v); err != nil {
		t.Fatal(err)
	}
	if d.current != defaultShortcut() {
		t.Fatal("task shortcut changed chat shortcut")
	}
	next := newService(&memoryStore{})
	next.configureTaskShortcut(path)
	if next.TaskShortcutSettings().Shortcut != v {
		t.Fatal("task shortcut did not persist")
	}
	bad := v
	bad.Key = "KeyC"
	d.rejectTasks = true
	if _, err := s.SaveTaskShortcut(bad); err == nil || d.tasks != v {
		t.Fatal("conflict displaced task shortcut")
	}
	d.rejectTasks = false
	s.taskShortcutFile = path + "/invalid"
	if _, err := s.SaveTaskShortcut(bad); err == nil || d.tasks != v {
		t.Fatal("failed persistence did not roll back")
	}
	s.ToggleTaskDock()
	if d.toggles != 1 {
		t.Fatal("missing preview action")
	}
	s.shutdown()
	s.ToggleTaskDock()
	if d.toggles != 1 {
		t.Fatal("preview reopened after shutdown")
	}
}
