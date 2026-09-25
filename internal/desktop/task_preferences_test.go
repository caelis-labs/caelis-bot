package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"path/filepath"
	"testing"
)

func TestTaskPreferencesMissingTerminalPersistsDefaultFallback(t *testing.T) {
	store, e := tasks.OpenPreferences(filepath.Join(t.TempDir(), "prefs.json"))
	if e != nil {
		t.Fatal(e)
	}
	s := newService(&memoryStore{value: defaults()})
	s.taskPreferences = store.Snapshot
	s.saveTaskPreferences = store.Save
	installed := true
	s.terminalChoices = func() []taskterminal.Choice { return taskterminal.Choices(func(string) bool { return installed }) }
	p, _ := s.TaskPreferences()
	p.Terminal = "iterm2"
	p.CustomCommand = "my-terminal {script}"
	if _, e = s.SaveTaskPreferences(p); e != nil {
		t.Fatal(e)
	}
	before := store.Snapshot()
	installed = false
	p, e = s.TaskPreferences()
	if e != nil || p.Terminal != "system" || p.Revision != before.Revision+1 || p.CustomCommand != before.CustomCommand || p != store.Snapshot() {
		t.Fatal(p, e)
	}
	again, _ := s.TaskPreferences()
	if again != p {
		t.Fatal("fallback rewrote an unchanged preference")
	}
	p.Terminal = "ghostty"
	p.MaxRunning = 9
	if p, e = s.SaveTaskPreferences(p); e != nil || p.Terminal != "system" || p.MaxRunning != 9 {
		t.Fatal(p, e)
	}
	p.Terminal = "custom"
	if p, e = s.SaveTaskPreferences(p); e != nil {
		t.Fatal(e)
	}
	if current, e := s.TaskPreferences(); e != nil || current.Terminal != "custom" {
		t.Fatal("custom configuration unexpectedly replaced", current, e)
	}
}
