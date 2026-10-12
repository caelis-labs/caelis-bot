//go:build darwin && cgo

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTerminalAssociationDoesNotNeedPermissionPlaceholderFile(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "task.command")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	want := readDefaultTerminalBundle(existing)
	if want == "" {
		t.Skip("this macOS environment has no .command application association")
	}
	missing := filepath.Join(root, "permission.command")
	if got := readDefaultTerminalBundle(missing); got != want {
		t.Fatalf("permission query lost system application association: got %q want %q", got, want)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read-only permission lookup created a placeholder file")
	}
}
