package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAttachmentCleanupOnlyTrashesOldManagedCopiesWhenIdle(t *testing.T) {
	s, _ := sessionPair(t, "")
	root := filepath.Join(s.opts.Directory, ".attachments")
	old := time.Now().Add(-31 * 24 * time.Hour)
	create := func(name string, when time.Time) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "0-fixture.txt")
		if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	oldDir := create("input-1234", old)
	recent := create("input-5678", time.Now())
	unmanaged := create("user-folder", old)
	outside := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := create("input-9101", old)
	if err := os.Symlink(outside, filepath.Join(linked, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "input-1112")); err != nil {
		t.Fatal(err)
	}
	info, err := s.AttachmentStorage()
	if err != nil || info.Files != 2 || info.EligibleFiles != 1 || !info.CanClean {
		t.Fatalf("%+v %v", info, err)
	}
	var moved []string
	trash := func(path string) error {
		moved = append(moved, path)
		return os.Rename(path, filepath.Join(t.TempDir(), filepath.Base(path)))
	}
	s.mu.Lock()
	s.binding.Pending = &pendingSubmission{ID: "unknown"}
	s.mu.Unlock()
	if _, err = s.TrashOldAttachments(testContext(t), trash); err == nil || len(moved) > 0 {
		t.Fatal("cleaned during unknown submission")
	}
	s.mu.Lock()
	s.binding.Pending = nil
	s.mu.Unlock()
	info, err = s.TrashOldAttachments(testContext(t), trash)
	if err != nil || len(moved) != 1 || moved[0] != oldDir || info.EligibleFiles != 0 {
		t.Fatalf("%v %+v %v", moved, info, err)
	}
	for _, p := range []string{recent, unmanaged, linked, outside} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatal("changed unrelated data", p, err)
		}
	}
}
