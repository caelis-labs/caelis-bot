package runtimeinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}
func TestVerifiedBrewFormulaUpgradeKeepsChannel(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "brew")
	bin := filepath.Join(prefix, "Cellar", "codex", "1.0", "bin", "codex")
	executable(t, bin, "exit 0")
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(prefix, "bin", "codex")); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls")
	t.Setenv("SOURCE_TEST_LOG", log)
	executable(t, filepath.Join(root, "bin", "brew"), `case "$1" in
 --prefix) printf '%s\n' "`+prefix+`";;
 list) if [ "$2 $3 $4" = '--formula --versions codex' ]; then echo 'codex 1.0'; fi;;
 upgrade) printf '%s\n' "$*" >> "$SOURCE_TEST_LOG";;
 esac`)
	t.Setenv("PATH", filepath.Join(root, "bin")+":/usr/bin:/bin")
	source := Detect(t.Context(), bin, "codex", "@openai/codex")
	if source.Kind != "brew-formula" {
		t.Fatalf("source=%+v", source)
	}
	if err := Upgrade(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if string(b) != "upgrade --formula codex\n" {
		t.Fatalf("unexpected manager operation %q", b)
	}
}
func TestVerifiedGlobalNPMUpgradeKeepsPrefix(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "node")
	modules := filepath.Join(prefix, "lib", "node_modules")
	packageDir := filepath.Join(modules, "@openai", "codex")
	executable(t, filepath.Join(packageDir, "bin", "codex"), "exit 0")
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"@openai/codex"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(prefix, "bin", "codex")
	if err := os.Symlink(filepath.Join(packageDir, "bin", "codex"), entry); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls")
	t.Setenv("SOURCE_TEST_LOG", log)
	executable(t, filepath.Join(root, "npm"), `case "$1 $2" in
 'root -g') printf '%s\n' "`+modules+`";;
 'prefix -g') printf '%s\n' "`+prefix+`";;
 'update -g') printf '%s\n' "$*" >> "$SOURCE_TEST_LOG";;
 esac`)
	t.Setenv("PATH", root+":/usr/bin:/bin")
	source := Detect(t.Context(), entry, "codex", "@openai/codex")
	if source.Kind != "npm" {
		t.Fatalf("source=%+v", source)
	}
	if err := Upgrade(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "update -g @openai/codex") {
		t.Fatalf("unexpected manager operation %q", b)
	}
	if source := Detect(t.Context(), filepath.Join(packageDir, "bin", "codex"), "caelis", "@caelis/caelis"); source.Kind != "unknown" {
		t.Fatalf("wrong package accepted: %+v", source)
	}
}
