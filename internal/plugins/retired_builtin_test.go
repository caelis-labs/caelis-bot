package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRetiredDesktopWorldDataIsPreservedWithoutSelection(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "versions", "desktop-world", "0.1.3-existing")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(legacy, "user-kept.txt")
	if err := os.WriteFile(file, []byte("original package bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	original := state{Version: 1, Revision: 9, Installed: map[string]installed{"desktop-world": {Root: filepath.Base(legacy), Enabled: true}}, Connections: map[string]connectionRecord{}}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "state.json")
	if err = os.WriteFile(statePath, body, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range m.Snapshot().Items {
		if item.ID == "desktop-world" {
			t.Fatal("Runtime capability exposed as a Bot-managed package")
		}
	}
	selected := m.Selection()
	for _, server := range selected.Servers {
		if server.PackageID == "desktop-world" {
			t.Fatal("legacy DTW server selected")
		}
	}
	for _, skill := range selected.SkillRoots {
		if filepath.Dir(filepath.Dir(skill)) == legacy {
			t.Fatal("legacy DTW Skill selected")
		}
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "original package bytes" {
		t.Fatal("legacy package changed", err)
	}
	got, err = os.ReadFile(statePath)
	if err != nil || string(got) != string(body) {
		t.Fatal("legacy state rewritten", err)
	}
	if err = RunStdio(root, "desktop-world", "desktop-world", nil, nil, nil, filepath.Base(legacy)); err == nil {
		t.Fatal("retired bridge could launch")
	}
}
