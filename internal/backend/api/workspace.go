package api

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrTaskWorkspaceUnavailable = errors.New("工作目录已不存在，请新建 Worker")

// ValidateTaskWorkspace checks a retained binding without changing its cwd or
// recreating it or following a replacement symlink. This is a canonical path
// binding, not inode/File ID identity: an ordinary directory recreated at the
// same path is indistinguishable from the original.
func ValidateTaskWorkspace(path string) error {
	if resolved, err := ResolveTaskWorkspace(path); err != nil || resolved != path {
		return ErrTaskWorkspaceUnavailable
	}
	return nil
}

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
