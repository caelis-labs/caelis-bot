package desktop

import "testing"

func TestWindowsHotkeyFailedReplacementKeepsOriginalRegistration(t *testing.T) {
	h := windowsHotkeys{}
	registered := map[int]uint32{}
	reg := func(id int, mods, key uint32) bool {
		if key == 'B' {
			return false
		}
		registered[id] = key
		return true
	}
	unreg := func(id int) bool { delete(registered, id); return true }
	a := Shortcut{Enabled: true, Key: "KeyA", Control: true}
	if err := h.replace(1, a, reg, unreg); err != nil {
		t.Fatal(err)
	}
	old := h.entries[1].id
	b := a
	b.Key = "KeyB"
	if err := h.replace(1, b, reg, unreg); err == nil {
		t.Fatal("conflict accepted")
	}
	if len(registered) != 1 || registered[old] != 'A' || h.logical(old) != 1 || h.entries[1].shortcut != a {
		t.Fatal("old shortcut was lost")
	}
	b.Key = "KeyC"
	if err := h.replace(1, b, reg, unreg); err != nil {
		t.Fatal(err)
	}
	if len(registered) != 1 || h.logical(old) != 0 || registered[h.entries[1].id] != 'C' {
		t.Fatal("replacement not atomic")
	}
	if err := h.replace(1, a, reg, unreg); err != nil {
		t.Fatal("save failure rollback", err)
	}
	a.Enabled = false
	if err := h.replace(1, a, reg, unreg); err != nil || len(registered) != 0 || len(h.entries) != 0 {
		t.Fatal("disabled key still registered", err)
	}
}

func TestWindowsTaskShortcutPeekDoesNotChangeHiddenPetPreference(t *testing.T) {
	d := windowsDriver{}
	d.taskCount.Store(1)
	if d.taskDockVisible() {
		t.Fatal("hidden pet passively exposed tasks")
	}
	d.changeTaskDockExpansion(true, true)
	if !d.taskDockVisible() || d.placement.Visible {
		t.Fatal("explicit task recall lost hidden preference")
	}
	d.changeTaskDockExpansion(false, false)
	if d.taskDockVisible() {
		t.Fatal("dismissed peek was revived")
	}
}
