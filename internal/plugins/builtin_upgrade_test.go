package plugins

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Opt-in release acceptance using staged, verified public packages. No MCP
// process, GUI action, model or user profile is started.
func TestReleasedDesktopWorldUpgrade(t *testing.T) {
	oldDir := os.Getenv("CAELIS_BOT_TEST_DTW_UPGRADE_OLD")
	newDir := os.Getenv("CAELIS_BOT_TEST_DTW_UPGRADE_NEW")
	if oldDir == "" || newDir == "" {
		t.Skip("set both staged Plugin directories for release upgrade acceptance")
	}
	read := func(dir string) (Entry, fs.FS) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(dir, "desktop-world-inventory.json"))
		if err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(dir, "desktop-world")
		entry, err := readDesktopWorldInventory(root, body)
		if err != nil {
			t.Fatal(err)
		}
		return entry, os.DirFS(root)
	}
	oldEntry, oldSource := read(oldDir)
	newEntry, newSource := read(newDir)
	if oldEntry.Version == newEntry.Version || len(newEntry.Executables) != 2 {
		t.Fatal("expected distinct release generations and adapter/worker")
	}
	for _, enabled := range []bool{true, false} {
		name := "enabled"
		if !enabled {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			store := t.TempDir()
			m, err := openCatalog(store, []Entry{oldEntry}, map[string]fs.FS{oldEntry.ID: oldSource})
			if err != nil {
				t.Fatal(err)
			}
			oldSelection := m.Selection()
			if len(oldSelection.SkillRoots) != 1 || len(oldSelection.Servers) != 1 {
				t.Fatal("old package selection", oldSelection.Issues)
			}
			oldRoot := oldSelection.Servers[0].Root
			if !enabled {
				if _, err := m.Mutate(t.Context(), oldEntry.ID, "disable", nil); err != nil {
					t.Fatal(err)
				}
			}
			m, err = openCatalog(store, []Entry{newEntry}, map[string]fs.FS{newEntry.ID: newSource})
			if err != nil {
				t.Fatal(err)
			}
			item := m.Snapshot().Items[0]
			if item.Version != newEntry.Version || item.Enabled != enabled {
				t.Fatal("upgrade lost release or enabled preference", item.Version, item.Enabled)
			}
			if !enabled {
				if s := m.Selection(); len(s.SkillRoots) != 0 || len(s.Servers) != 0 {
					t.Fatal("disabled upgrade exposed tools or Skills")
				}
				if _, err := m.Mutate(t.Context(), newEntry.ID, "enable", nil); err != nil {
					t.Fatal(err)
				}
			}
			s := m.Selection()
			if len(s.SkillRoots) != 1 || len(s.Servers) != 1 || len(s.Issues) != 0 {
				t.Fatal("upgraded package selection", s.Issues)
			}
			newRoot := s.Servers[0].Root
			if newRoot == oldRoot || s.SkillRoots[0] != filepath.Join(newRoot, "skills", "desktop-world") {
				t.Fatal("binary and Skill did not move together")
			}
			// Verify all bytes, including Skill references, licensing and both
			// native executables; the previous immutable generation stays intact.
			if err := Verify(newRoot, newEntry.Files); err != nil {
				t.Fatal(err)
			}
			if err := Verify(oldRoot, oldEntry.Files); err != nil {
				t.Fatal("previous generation changed", err)
			}
			for _, path := range newEntry.Executables {
				if newEntry.Files[path] == oldEntry.Files[path] {
					t.Fatal("native release binary was not replaced", path)
				}
			}
			// Read only the installed adapter version on the matching host OS.
			exe := newEntry.Executables[0]
			if runtime.GOOS == "darwin" && exe == "bin/dtw" || runtime.GOOS == "windows" && exe == "bin/dtw.exe" {
				out, err := exec.CommandContext(t.Context(), filepath.Join(newRoot, exe), "--version").CombinedOutput()
				if err != nil || !strings.HasPrefix(string(out), "dtw "+newEntry.Version+" ") {
					t.Fatalf("installed adapter version: %s (%v)", out, err)
				}
				t.Log(strings.TrimSpace(string(out)))
			}
			t.Logf("%s -> %s; new Skill SHA256 %s", oldEntry.Version, newEntry.Version, newEntry.Files["skills/desktop-world/SKILL.md"])
			// Reopening must reuse the same installed generation.
			again, err := openCatalog(store, []Entry{newEntry}, map[string]fs.FS{newEntry.ID: newSource})
			if err != nil {
				t.Fatal(err)
			}
			if got := again.Selection(); len(got.Servers) != 1 || got.Servers[0].Root != newRoot {
				t.Fatal("unchanged release installed another generation")
			}
		})
	}
}
