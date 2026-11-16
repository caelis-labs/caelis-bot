package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedWorkspaceUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "工作 space")
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
