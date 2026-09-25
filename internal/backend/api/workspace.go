package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolveTaskWorkspace resolves an existing user-selected directory without
// creating, chmod-ing or deleting anything in it. Persist this canonical path as
// the native cwd and sandbox root; an alias must not retarget a resumed task.
func ResolveTaskWorkspace(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("workspace must be an absolute existing directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace must be a directory")
	}
	return resolved, nil
}
