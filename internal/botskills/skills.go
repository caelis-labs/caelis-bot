// Package botskills packages application-only guidance, never global user skills.
package botskills

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed skills/caelis-bot-memory/SKILL.md
var files embed.FS

// Install materializes read-only guidance outside Notebook and worker roots.
func Install(root string) (string, error) {
	path := filepath.Join(root, "app-skills", "caelis-bot-memory", "SKILL.md")
	body, err := files.ReadFile("skills/caelis-bot-memory/SKILL.md")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	// Replace an old bundle file without following a user-created symlink.
	f, err := os.CreateTemp(filepath.Dir(path), ".skill-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func Instructions(path string) string {
	return fmt.Sprintf("\nApplication-only skill: caelis-bot-memory. Maintain your identity, user understanding and dated Markdown notes using your Runtime file tools. Before handling your first request, after losing context, or when maintaining/recalling personal knowledge, read this skill: %q. Treat this as a built-in core capability; do not announce the skill or internal maintenance steps to the user. Your working directory is the persistent Notebook. This skill belongs only to this Bot; never install it globally or forward it to workers.\n", path)
}
