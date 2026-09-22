package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSelectionIsLocalAtomicAndDeduplicated(t *testing.T) {
	s, _, _ := setup()
	dir := t.TempDir()
	var paths []string
	for i := range 9 {
		path := filepath.Join(dir, fmt.Sprintf("附件-%d.txt", i))
		if err := os.WriteFile(path, []byte("private contents"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	s.pickFiles = func() ([]string, error) { return []string{paths[0], paths[0]}, nil }
	files, err := s.PickFiles()
	if err != nil || len(files) != 1 || files[0].Name != "附件-0.txt" {
		t.Fatalf("selection: %+v %v", files, err)
	}
	jsonBytes, _ := json.Marshal(files)
	if strings.Contains(string(jsonBytes), dir) || strings.Contains(string(jsonBytes), "private contents") {
		t.Fatal("private path or bytes exposed")
	}
	s.pickFiles = func() ([]string, error) { return paths, nil }
	if _, err := s.PickFiles(); err == nil || len(s.DraftFiles()) != 1 {
		t.Fatal("oversized batch partially changed draft")
	}
	s.pickFiles = func() ([]string, error) { return []string{paths[1], dir}, nil }
	if _, err := s.PickFiles(); err == nil || len(s.DraftFiles()) != 1 {
		t.Fatal("non-file batch partially changed draft")
	}
	s.pickFiles = func() ([]string, error) { return nil, nil }
	if result, err := s.PickFiles(); err != nil || len(result) != 1 {
		t.Fatal("cancel removed selection")
	}
	if remaining, err := s.RemoveFile(files[0].ID); err != nil || len(remaining) != 0 {
		t.Fatal("remove failed")
	}
}

func TestPickerDoesNotHoldLifecycleAndCannotCompleteAfterShutdown(t *testing.T) {
	s, d, _ := setup()
	entered, release := make(chan struct{}), make(chan struct{})
	s.pickFiles = func() ([]string, error) { close(entered); <-release; return nil, nil }
	done := make(chan error, 1)
	go func() { _, err := s.PickFiles(); done <- err }()
	<-entered
	if _, err := s.PickFiles(); err == nil {
		t.Fatal("duplicate picker allowed")
	}
	s.ClosePanel()
	s.shutdown()
	if !d.stopped {
		t.Fatal("picker blocked shutdown")
	}
	close(release)
	if <-done == nil {
		t.Fatal("late picker accepted after shutdown")
	}
}

func TestSelectedFilesSurviveRestartAndMissingFilesRemainRemovable(t *testing.T) {
	dir := t.TempDir()
	selection := filepath.Join(dir, "selection.json")
	file := filepath.Join(dir, "附件.txt")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.pickFiles = func() ([]string, error) { return []string{file}, nil }
	files, err := s.PickFiles()
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := setup()
	if err := restored.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	got := restored.DraftFiles()
	if len(got) != 1 || got[0].ID != files[0].ID || got[0].Unavailable {
		t.Fatal("selection not restored")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if !restored.DraftFiles()[0].Unavailable {
		t.Fatal("missing file not identified")
	}
	if _, err := restored.RemoveFile(got[0].ID); err != nil {
		t.Fatal(err)
	}
	next, _, _ := setup()
	if err := next.configureSelection(selection); err != nil || len(next.DraftFiles()) != 0 {
		t.Fatal("removed file restored", err)
	}
	info, _ := os.Stat(selection)
	if info.Mode().Perm() != 0600 {
		t.Fatal("selection path metadata not private")
	}
}

func TestAcceptedFileConsumptionPersists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	path := filepath.Join(dir, "selection.json")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	s, _, _ := setup()
	_ = s.configureSelection(path)
	s.pickFiles = func() ([]string, error) { return []string{file}, nil }
	files, err := s.PickFiles()
	if err != nil {
		t.Fatal(err)
	}
	s.consumeDraftFiles([]string{files[0].ID})
	next, _, _ := setup()
	if err := next.configureSelection(path); err != nil || len(next.DraftFiles()) != 0 {
		t.Fatal("accepted selection restored", err)
	}
}
