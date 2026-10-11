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
	for _, name := range []string{"caelis-bot-memory", "caelis-dream", "bot-dream", "custom-guide"} {
		legacy := filepath.Join(root, "app-skills", name)
		if err := os.MkdirAll(legacy, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "SKILL.md"), []byte("old or user content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"caelis-bot-memory", "caelis-dream", "bot-dream"} {
		if _, err := os.Stat(filepath.Join(root, "app-skills", old)); !os.IsNotExist(err) {
			t.Fatalf("old system skill retained: %s: %v", old, err)
		}
	}
	if _, err := os.ReadFile(filepath.Join(root, "app-skills", "custom-guide", "SKILL.md")); err != nil {
		t.Fatal("custom skill changed", err)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	name, description := metadata(string(body))
	if name != "bot-core" || description == "" || len(description) > 1024 {
		t.Fatal("invalid bundled metadata")
	}
	catalog := Instructions(p)
	if strings.Contains(catalog, "bot-dream") {
		t.Fatal("retired maintenance skill remained in catalog")
	}
	if !strings.Contains(catalog, description) || !strings.Contains(catalog, p) || strings.Contains(catalog, "# Restore your context") || strings.Contains(catalog, "MEMORY.md") {
		t.Fatal("metadata missing or body eagerly injected", catalog)
	}
	links := regexp.MustCompile(`\]\((references/[^)]+)\)`).FindAllSubmatch(body, -1)
	if len(links) != 11 || !strings.Contains(string(body), "references/recovery.md") || !strings.Contains(string(body), "references/weixin-setup.md") || !strings.Contains(string(body), "references/plugins.md") || strings.Contains(string(body), "bot_desktop_") || strings.Contains(string(body), "Desktop World") {
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
		// Conditional workflows must resolve from the installed body,
		// not depend on checkout paths or eager injection into the catalog.
		if strings.HasSuffix(name, ".md") {
			for _, link := range regexp.MustCompile(`\]\(([^):]+\.md)\)`).FindAllSubmatch(installed, -1) {
				if _, err := os.ReadFile(filepath.Join(root, "app-skills", filepath.Dir(strings.TrimPrefix(name, "skills/")), string(link[1]))); err != nil {
					t.Errorf("broken progressive link in %s: %s: %v", name, link[1], err)
				}
			}
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
