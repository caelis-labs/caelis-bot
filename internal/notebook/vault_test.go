package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlainFilesIndexRebuildAndDeletion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Notebook")
	v, err := OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 9, 23, 23, 59, 0, 0, time.FixedZone("local", 8*3600))
	if err = v.Refresh(t.Context(), day); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(dir, "2026", "09", "23", "plan with space.md")
	body := "# A [plan]\n\nExternal editor content without private metadata.\n"
	if err = os.WriteFile(note, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	memory := strings.Repeat("long user core memory\n", 600)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(memory), 0600)
	os.WriteFile(filepath.Join(dir, "INDEX.md"), []byte("broken index"), 0600)
	if err = v.Refresh(t.Context(), day.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if !strings.HasPrefix(string(index), IndexNotice) || !strings.Contains(string(index), "plan%20with%20space.md") || !strings.Contains(string(index), `A \[plan\]`) {
		t.Fatal(string(index))
	}
	if _, err = os.Stat(filepath.Join(dir, "2026", "09", "24")); err != nil {
		t.Fatal("local next day missing", err)
	}
	if b, _ := os.ReadFile(note); string(b) != body {
		t.Fatal("note rewritten")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md")); string(b) != memory {
		t.Fatal("core memory truncated")
	}
	os.Remove(note)
	os.Remove(filepath.Join(dir, "MEMORY.md"))
	v.Close()
	v, err = OpenVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if _, err = os.Stat(filepath.Join(dir, "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatal("deleted memory resurrected")
	}
	index, _ = os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if strings.Contains(string(index), "plan") || strings.Contains(string(index), "MEMORY.md") {
		t.Fatal("stale index after deletion")
	}
}
func TestIndexDoesNotReadOrOverwriteSymlinkTargets(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside.md")
	os.WriteFile(outside, []byte("outside-secret"), 0600)
	v, err := OpenVault(filepath.Join(parent, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err = os.Symlink(outside, filepath.Join(v.Path(), "linked.md")); err != nil {
		t.Skip(err)
	}
	os.Remove(filepath.Join(v.Path(), "INDEX.md"))
	os.Symlink(outside, filepath.Join(v.Path(), "INDEX.md"))
	if err = v.Refresh(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(v.Path(), "INDEX.md"))
	if strings.Contains(string(index), "outside") || strings.Contains(string(index), "linked") {
		t.Fatal("symlink content indexed")
	}
	if body, _ := os.ReadFile(outside); string(body) != "outside-secret" {
		t.Fatal("target overwritten")
	}
}
func TestLegacyMigrationCopiesOnceWithoutResurrectingDeletedImports(t *testing.T) {
	parent := t.TempDir()
	old := filepath.Join(parent, "personal", "notebook")
	os.MkdirAll(old, 0700)
	raw := `<!-- caelis-note {"version":1,"title":"Legacy","updated":"2026-09-22T16:30:00Z","source":"user"} -->` + "\n\nOriginal note"
	os.WriteFile(filepath.Join(old, "one.md"), []byte(raw), 0600)
	v, err := OpenVault(filepath.Join(parent, "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	marker := filepath.Join(parent, "migration.json")
	day := time.Date(2026, 9, 23, 12, 0, 0, 0, time.FixedZone("local", 8*3600))
	if err = v.Migrate(old, marker, "# Legacy profile\n", day); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(v.Path(), "2026", "09", "23", "imported-one.md")
	body, err := os.ReadFile(imported)
	if err != nil || !strings.Contains(string(body), "Original note") || strings.Contains(string(body), "caelis-note") {
		t.Fatal("migration", err)
	}
	if b, _ := os.ReadFile(filepath.Join(old, "one.md")); string(b) != raw {
		t.Fatal("original changed")
	}
	os.Remove(imported)
	if err = v.Migrate(old, marker, "# Changed legacy profile", day.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(imported); !os.IsNotExist(err) {
		t.Fatal("deleted import resurrected")
	}
}
