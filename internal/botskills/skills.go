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

// Install replaces only the two host-managed skill directories. Other entries
// under app-skills, Notebook, plugin packages, and custom skills are untouched.
func Install(root string) (string, error) {
	appRoot := filepath.Join(root, "app-skills")
	if err := os.MkdirAll(appRoot, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(appRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("app-skills must be a real directory")
	}
	stage, err := os.MkdirTemp(appRoot, ".bundle-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	err = fs.WalkDir(files, "skills", func(name string, entry fs.DirEntry, err error) error {
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
		return installFile(filepath.Join(stage, filepath.FromSlash(strings.TrimPrefix(name, "skills/"))), body)
	})
	if err != nil {
		return "", err
	}
	for _, name := range []string{"bot-core", "bot-dream"} {
		target := filepath.Join(appRoot, name)
		backup := filepath.Join(stage, ".previous-"+name)
		if _, err := os.Lstat(target); err == nil {
			if err := os.Rename(target, backup); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(filepath.Join(stage, name), target); err != nil {
			if _, oldErr := os.Lstat(backup); oldErr == nil {
				_ = os.Rename(backup, target)
			}
			return "", err
		}
	}
	for _, old := range []string{"caelis-bot-memory", "caelis-dream"} {
		if err := os.RemoveAll(filepath.Join(appRoot, old)); err != nil {
			return "", err
		}
	}
	// A previous Runtime's confirmed directory is not evidence for this one.
	if err := os.Remove(filepath.Join(appRoot, "mcp-tools.json")); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Join(appRoot, "bot-core", "SKILL.md"), nil
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
	var out strings.Builder
	out.WriteString("\n## Skills\n\nSkills provide instructions in SKILL.md files. Read a skill's file when its description applies, then follow linked references only as needed. Resolve relative references from that skill's directory.\n\n")
	for _, skill := range []string{"bot-core", "bot-dream"} {
		body, err := files.ReadFile(path.Join("skills", skill, "SKILL.md"))
		if err != nil {
			panic(err)
		}
		name, description := metadata(string(body))
		location := filepath.Join(filepath.Dir(filepath.Dir(skillPath)), skill, "SKILL.md")
		fmt.Fprintf(&out, "- %s: %s (file: %s)\n", name, description, location)
	}
	return out.String()
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
