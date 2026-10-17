package caelisruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsExplicitExecutableDoesNotRequirePOSIXMode(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "运行 工具.EXE")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := Find(path); err != nil || found != path {
		t.Fatal("native EXE rejected", err)
	}
	if err = os.WriteFile(path, []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Find(path); err == nil {
		t.Fatal("invalid EXE accepted")
	}
	wrapper := filepath.Join(filepath.Dir(path), "caelis.cmd")
	if err = os.WriteFile(wrapper, []byte("@echo off"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Find(wrapper); err == nil {
		t.Fatal("shell wrapper accepted")
	}
}
