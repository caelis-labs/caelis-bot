//go:build darwin || linux

package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallObservesExistingCLIWithoutReplacingIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	path := filepath.Join(home, ".local", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("#!/bin/sh\nprintf 'codex-cli 99.1.0\\n'\n")
	if err := os.WriteFile(path, original, 0700); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	status, err := ManageRuntime(t.Context(), "install", "")
	if err != nil || !status.Installed || status.Version != "99.1.0" {
		t.Fatal(status, err)
	}
	after, _ := os.Stat(path)
	data, _ := os.ReadFile(path)
	if !os.SameFile(before, after) || string(data) != string(original) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("replaced an existing external CLI")
	}
	if _, err := ManageRuntime(t.Context(), "update", path); err == nil {
		t.Fatal("updated an explicitly configured program")
	}
}
