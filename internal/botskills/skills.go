// Package botskills packages application-only guidance, never global user skills.
package botskills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed skills
var files embed.FS

// Install materializes read-only guidance outside Notebook and worker roots.
func Install(root string) (string, error) {
	path := filepath.Join(root, "app-skills", "caelis-bot-memory", "SKILL.md")
	err := fs.WalkDir(files, "skills", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		return installFile(filepath.Join(root, "app-skills", filepath.FromSlash(strings.TrimPrefix(name, "skills/"))), body)
	})
	return path, err
}

func installFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Replace an old bundle file without following a user-created symlink.
	f, err := os.CreateTemp(filepath.Dir(path), ".skill-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return nil
}

// Instructions exposes only standard skill metadata and a file locator. Both
// application adapters carry this scoped catalog on start/resume. Reading the
// body/references is a model tool action, never automatic prefix assembly.
func Instructions(skillPath string) string {
	body, err := files.ReadFile(path.Join("skills", "caelis-bot-memory", "SKILL.md"))
	if err != nil {
		panic(err)
	} // Embedded application asset.
	name, description := metadata(string(body))
	return fmt.Sprintf("\n## Skills\n\nSkills provide instructions in SKILL.md files. Read a skill's file when its description applies, then follow linked references only as needed. Resolve relative references from that skill's directory.\n\n- %s: %s (file: %s)\n", name, description, skillPath)
}

// The bundled frontmatter deliberately uses simple single-line scalar values.
// Keep its metadata in one source; bundle tests reject unsupported formatting.
func metadata(body string) (name, description string) {
	_, front, ok := strings.Cut(body, "---\n")
	if !ok {
		return
	}
	front, _, _ = strings.Cut(front, "\n---")
	for line := range strings.SplitSeq(front, "\n") {
		key, value, _ := strings.Cut(line, ": ")
		switch key {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	return
}
