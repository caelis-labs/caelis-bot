package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedWorkspaceUnavailable(t *testing.T) {
	root, err := ResolveTaskWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "工作 space")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskWorkspace(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskWorkspace(path); !errors.Is(err, ErrTaskWorkspaceUnavailable) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing workspace was recreated", err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskWorkspace(path); !errors.Is(err, ErrTaskWorkspaceUnavailable) {
		t.Fatal("file accepted as workspace", err)
	}
}

func TestRetainedWorkspaceCannotFollowReplacementSymlink(t *testing.T) {
	root, err := ResolveTaskWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "original")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTaskWorkspace(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ValidateTaskWorkspace(path); !errors.Is(err, ErrTaskWorkspaceUnavailable) {
		t.Fatal("bound path followed a replacement symlink", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	// The contract retains a canonical path, not an inode/File ID.
	if err := ValidateTaskWorkspace(path); err != nil {
		t.Fatal("ordinary replacement at the same path is indistinguishable", err)
	}
}
