package productrpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateUploadsPersistAndRejectChangedFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Product", "Uploads")
	s, err := OpenUploads(root)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte("synthetic file")
	sum := sha256.Sum256(b)
	meta, err := s.Upload(t.Context(), Resource{Name: "notes.txt", Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:])}, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	s, err = OpenUploads(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.Resolve([]string{meta.ID})
	if err != nil || len(files) != 1 || files[0].Name != "notes.txt" {
		t.Fatal(files, err)
	}
	if err = os.WriteFile(files[0].Path, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve([]string{meta.ID}); err == nil {
		t.Fatal("changed bytes selected")
	}
	if err = os.Remove(files[0].Path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err = os.WriteFile(outside, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, files[0].Path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Resolve([]string{meta.ID}); err == nil {
		t.Fatal("symlinked input file selected")
	}
	if _, _, err = s.Open(t.Context(), meta.ID); err == nil {
		t.Fatal("symlinked download opened")
	}
}

func TestPrivateUploadsRejectSymlinkedOrPublicRootsAndCatalog(t *testing.T) {
	outside := t.TempDir()
	base := t.TempDir()
	root := filepath.Join(base, "Uploads")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUploads(root); err == nil {
		t.Fatal("linked root accepted")
	}
	parent := filepath.Join(base, "Product")
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUploads(filepath.Join(parent, "Uploads")); err == nil {
		t.Fatal("linked parent accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "Uploads")); !os.IsNotExist(err) {
		t.Fatal("unsafe parent created files outside profile")
	}
	public := filepath.Join(base, "Public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUploads(filepath.Join(public, "Uploads")); err == nil {
		t.Fatal("public parent accepted")
	}
	private := filepath.Join(base, "Private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "catalog.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUploads(private); err == nil {
		t.Fatal("public catalog accepted")
	}
}
