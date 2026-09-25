package botskills

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBundleInstallsReferencesAndExposesOnlyMetadata(t *testing.T) {
	root := t.TempDir()
	p, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	name, description := metadata(string(body))
	if name != "caelis-bot-memory" || description == "" || len(description) > 1024 {
		t.Fatal("invalid bundled metadata")
	}
	catalog := Instructions(p)
	if !strings.Contains(catalog, description) || !strings.Contains(catalog, p) || strings.Contains(catalog, "# Restore your context") || strings.Contains(catalog, "MEMORY.md") {
		t.Fatal("metadata missing or body eagerly injected", catalog)
	}
	links := regexp.MustCompile(`\]\((references/[^)]+)\)`).FindAllSubmatch(body, -1)
	if len(links) != 4 {
		t.Fatal("missing progressive routes")
	}
	for _, link := range links {
		if _, err := os.ReadFile(filepath.Join(filepath.Dir(p), string(link[1]))); err != nil {
			t.Fatal(err)
		}
	}
	err = fs.WalkDir(files, "skills", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		packaged, _ := files.ReadFile(name)
		installed, e := os.ReadFile(filepath.Join(root, "app-skills", strings.TrimPrefix(name, "skills/")))
		if e != nil || string(packaged) != string(installed) {
			t.Errorf("incomplete bundle: %s %v", name, e)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Installation stays outside both personal notes and independent task roots.
	for _, dir := range []string{"Notebook", "Tasks", ".agents", ".codex"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			t.Fatal("skill escaped application scope", dir)
		}
	}
}
