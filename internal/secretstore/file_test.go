package secretstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreMigratesOnceAndSeparatesConsumers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Credentials")
	loads, deletes := 0, 0
	legacy := Functions{LoadFunc: func(id string) (string, error) {
		loads++
		if id != "same-id" {
			t.Fatal("wrong legacy key")
		}
		return "private-token", nil
	}, DeleteFunc: func(id string) error { deletes++; return nil }}
	weixin := &FileStore{Root: root, Namespace: "weixin", Legacy: legacy}
	plugins := &FileStore{Root: root, Namespace: "plugins"}
	telegram := &FileStore{Root: root, Namespace: "telegram"}
	if value, err := weixin.Load("same-id"); err != nil || value != "private-token" {
		t.Fatalf("migration: %v", err)
	}
	if err := plugins.Save("same-id", "api-key"); err != nil {
		t.Fatal(err)
	}
	if err := telegram.Save("same-id", "bot-token"); err != nil {
		t.Fatal(err)
	}
	for store, want := range map[*FileStore]string{weixin: "private-token", plugins: "api-key", telegram: "bot-token"} {
		value, err := store.Load("same-id")
		if err != nil || value != want {
			t.Fatalf("namespace isolation: %v", err)
		}
		path, _ := store.path("same-id")
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private file permissions: %v %v", info, err)
		}
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil || parent.Mode().Perm() != 0700 {
			t.Fatalf("private directory permissions: %v %v", parent, err)
		}
	}
	if loads != 1 || deletes != 1 {
		t.Fatalf("legacy reused: loads=%d deletes=%d", loads, deletes)
	}
	if err := plugins.Delete("same-id"); err != nil {
		t.Fatal(err)
	}
	if value, err := weixin.Load("same-id"); err != nil || value != "private-token" {
		t.Fatal("delete crossed namespace")
	}
}

func TestFileStoreRejectsUnsafeFileAndIncompleteMigration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Credentials")
	loads := 0
	store := &FileStore{Root: root, Namespace: "weixin", Legacy: Functions{LoadFunc: func(string) (string, error) {
		loads++
		return "private-token", nil
	}, DeleteFunc: func(string) error { return errors.New("legacy delete failed") }}}
	path, _ := store.path("id")
	if _, err := store.Load("id"); err == nil {
		t.Fatal("accepted incomplete migration")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("left second credential copy")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"secret":"unsafe"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("id"); err == nil || loads != 1 {
		t.Fatal("unsafe file fell back to legacy store")
	}
}

func TestFileStoreNewSaveDeletesLegacyAndRefusesUnsafePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Credentials")
	deletes := 0
	store := &FileStore{Root: root, Namespace: "telegram", Legacy: Functions{DeleteFunc: func(string) error { deletes++; return nil }}}
	if err := store.Save("token", "new-value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("token", "replacement"); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 {
		t.Fatalf("legacy delete count: %d", deletes)
	}
	path, _ := store.path("token")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "other"), path); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("token", "changed"); err == nil {
		t.Fatal("overwrote symlink")
	}
}

func TestFileStoreRejectsSymlinkedNamespace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Credentials")
	outside := t.TempDir()
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "plugins")); err != nil {
		t.Fatal(err)
	}
	store := &FileStore{Root: root, Namespace: "plugins"}
	if err := store.Save("id", "secret"); err == nil {
		t.Fatal("wrote through symlinked namespace")
	}
	if _, err := store.Load("id"); err == nil {
		t.Fatal("read through symlinked namespace")
	}
	if err := store.Delete("id"); err == nil {
		t.Fatal("deleted through symlinked namespace")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatal("secret escaped namespace")
	}
}
