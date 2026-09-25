package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesPersistValidateAndFenceStaleWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-preferences.json")
	s, e := OpenPreferences(path)
	if e != nil {
		t.Fatal(e)
	}
	p := s.Snapshot()
	if p.MaxRunning != 3 || p.Terminal != "system" {
		t.Fatal(p)
	}
	p.MaxRunning = 12
	p.Terminal = "ghostty"
	next, e := s.Save(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Save(p); e == nil {
		t.Fatal("stale save overwrote new preferences")
	}
	for _, bad := range []Preferences{{MaxRunning: 0, Terminal: "system", Revision: next.Revision}, {MaxRunning: -1, Terminal: "system", Revision: next.Revision}, {MaxRunning: 1, Terminal: "shell-command", Revision: next.Revision}} {
		if _, e = s.Save(bad); e == nil {
			t.Fatal("invalid preferences")
		}
	}
	reopened, e := OpenPreferences(path)
	if e != nil || reopened.Snapshot() != next {
		t.Fatal(e)
	}
	s.path = t.TempDir()
	p = next
	p.MaxRunning = 4
	if _, e = s.Save(p); e == nil || s.Snapshot() != next {
		t.Fatal("failed save changed admission")
	}
	if e = os.WriteFile(path, []byte(`{"maxRunning":0}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenPreferences(path); e == nil {
		t.Fatal("corrupt settings silently reset")
	}
}

func TestCustomTerminalDraftRequiresValidTemplateBeforeActivation(t *testing.T) {
	s, err := OpenPreferences(filepath.Join(t.TempDir(), "prefs.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := s.Snapshot()
	p.CustomCommand = "unfinished command"
	p, err = s.Save(p)
	if err != nil {
		t.Fatal("inactive draft rejected", err)
	}
	p.Terminal = "custom"
	if _, err = s.Save(p); err == nil || s.Snapshot().Terminal != "system" {
		t.Fatal("invalid command activated")
	}
	p.CustomCommand = `"/Applications/My Terminal.app/run" -- /bin/sh {script}`
	p, err = s.Save(p)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPreferences(s.path)
	if err != nil || reopened.Snapshot() != p {
		t.Fatal("custom preference not retained", err)
	}
}
