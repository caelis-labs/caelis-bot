package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopOwnerLeaseFencesDetachedRuntime(t *testing.T) {
	root := t.TempDir()
	if desktopOwnerActive(root) {
		t.Fatal("owner reported before Bot starts")
	}
	owner, err := lockDesktopOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	if !desktopOwnerActive(root) {
		t.Fatal("owner lock not visible to MCP bridge")
	}
	if second, err := lockDesktopOwner(root); err == nil {
		_ = second.Close()
		t.Fatal("second Bot owner acquired the same store")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if desktopOwnerActive(root) {
		t.Fatal("stale Runtime could keep executing after Bot exit")
	}
	if err := os.Remove(filepath.Join(root, desktopOwnerFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, desktopOwnerFile)); err != nil {
		t.Fatal(err)
	}
	if owner, err := lockDesktopOwner(root); err == nil {
		_ = owner.Close()
		t.Fatal("symlinked owner lock accepted")
	}
	if desktopOwnerActive(root) {
		t.Fatal("symlinked lock impersonated Bot owner")
	}
}

func TestDesktopWorldBridgeRequiresResidentOwner(t *testing.T) {
	root := t.TempDir()
	source := fixturePackage(t, false, true)
	if err := os.WriteFile(filepath.Join(source, "plugin.json"), []byte(`{"$schema":"`+manifestSchema+`","name":"desktop-world","version":"0.1.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	if err := filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		rel, _ := filepath.Rel(source, path)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entry := Entry{ID: "desktop-world", Title: "Desktop World", Version: "0.1.0", Source: "bundled:test", Files: files, Executables: []string{"bin/check"}}
	m, err := openCatalog(root, []Entry{entry}, map[string]fs.FS{"desktop-world": os.DirFS(source)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Mutate(t.Context(), "desktop-world", "install", nil); err != nil {
		t.Fatal(err)
	}
	selected := m.Selection().Servers[0]
	launch := func() error {
		return runStdio(t.Context(), m, "desktop-world", "local-validator", strings.NewReader(""), io.Discard, io.Discard, filepath.Base(selected.Root))
	}
	if err := launch(); err == nil || !strings.Contains(err.Error(), "Bot owner") {
		t.Fatal("detached Runtime started Desktop World", err)
	}
	if err := m.AcquireDesktopOwner(); err != nil {
		t.Fatal(err)
	}
	if err := launch(); err != nil {
		t.Fatal("resident-owned bridge failed", err)
	}
	m.CloseDesktopOwner()
	if err := launch(); err == nil || !strings.Contains(err.Error(), "Bot owner") {
		t.Fatal("stale Runtime restarted Desktop World", err)
	}
}
