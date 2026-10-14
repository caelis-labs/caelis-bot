package desktop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDraftPreviewPreservesOriginalImageAndSelectionAuthority(t *testing.T) {
	s, _, _ := setup()
	if err := s.configureSelection(filepath.Join(t.TempDir(), "draft-files.json")); err != nil {
		t.Fatal(err)
	}
	imageValue := image.NewNRGBA(image.Rect(0, 0, 800, 600))
	imageValue.Set(413, 217, color.NRGBA{R: 200, G: 90, B: 40, A: 127})
	var original bytes.Buffer
	if err := png.Encode(&original, imageValue); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, original.Bytes(), nil }
	result, err := s.PasteAttachments()
	if err != nil || len(result.Files) != 1 {
		t.Fatal(result, err)
	}
	id := result.Files[0].ID
	preview, err := s.DraftImagePreview(id)
	if err != nil || !strings.HasPrefix(preview, "data:image/png;base64,") {
		t.Fatal("original preview format", err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, "data:image/png;base64,"))
	if err != nil || !bytes.Equal(data, original.Bytes()) {
		t.Fatal("preview reduced or recompressed original screenshot detail", err)
	}
	thumbnail, err := s.DraftImage(id)
	if err != nil {
		t.Fatal(err)
	}
	thumbBytes, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(thumbnail, "data:image/jpeg;base64,"))
	cfg, _, err := image.DecodeConfig(bytes.NewReader(thumbBytes))
	if err != nil || cfg.Width > 160 || cfg.Height > 120 {
		t.Fatal("attachment chip lost its bounded thumbnail", cfg, err)
	}
	if files := s.DraftFiles(); len(files) != 1 || files[0].ID != id {
		t.Fatal("preview consumed or replaced the draft selection", files)
	}
	if _, err = s.DraftImagePreview("foreign-id"); err == nil {
		t.Fatal("unselected image was readable")
	}
	if err = os.Truncate(s.files[0].path, maxDraftFile+1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DraftImagePreview(id); err == nil {
		t.Fatal("changed file bypassed native size validation")
	}
	if _, err = s.RemoveFile(id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DraftImagePreview(id); err == nil {
		t.Fatal("removed selection still authorized a preview")
	}
}

func TestPickerNotifiesCommittedSelectionWithoutHoldingLifecycleLock(t *testing.T) {
	s, _, _ := setup()
	root := t.TempDir()
	if err := s.configureSelection(filepath.Join(root, "draft-files.json")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("synthetic attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	s.pickFiles = func() ([]string, error) { close(entered); <-release; return []string{path}, nil }
	notified := make(chan []DraftFile, 1)
	s.filesChanged = func() { notified <- s.DraftFiles() }
	done := make(chan error, 1)
	go func() { _, err := s.PickFiles(); done <- err }()
	<-entered
	// A replacement composer may read while the original native sheet is open.
	if files := s.DraftFiles(); len(files) != 0 {
		t.Fatal("sheet staged an unconfirmed selection")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if files := <-notified; len(files) != 1 || files[0].Name != "file.txt" {
		t.Fatal("new view was not notified of the committed original selection", files)
	}
}

func pastePNG(t *testing.T) []byte {
	t.Helper()
	value := image.NewRGBA(image.Rect(0, 0, 2, 2))
	value.Set(0, 0, color.RGBA{R: 230, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRuntimeSelectionKeepsUnsentFileAndClipboardImage(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first", "draft-files.json")
	second := filepath.Join(root, "second", "draft-files.json")
	s, _, _ := setup()
	if err := s.configureSelection(first); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "reference.txt")
	if err := os.WriteFile(file, []byte("unsent"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.stageFiles([]string{file}); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, pastePNG(t), nil }
	if _, err := s.PasteAttachments(); err != nil {
		t.Fatal(err)
	}
	oldOwned := s.files[1].path
	if err := s.switchSelection(second, true); err != nil {
		t.Fatal(err)
	}
	if files := s.DraftFiles(); len(files) != 2 || files[0].Unavailable || files[1].Unavailable {
		t.Fatalf("draft lost after switch: %+v", files)
	}
	if s.files[1].path == oldOwned {
		t.Fatal("clipboard image still owned by previous connection")
	}
	if _, err := os.Stat(oldOwned); err != nil {
		t.Fatal("rollback copy lost", err)
	}
	if err := s.switchSelection(first, true); err == nil {
		t.Fatal("two unsent selections silently replaced")
	}
	if s.selectionFile != second || len(s.DraftFiles()) != 2 {
		t.Fatal("failed switch changed active draft")
	}
	if err := s.switchSelection(first, false); err != nil {
		t.Fatal("rollback could not restore original selection", err)
	}
	if s.files[1].path != oldOwned || len(s.DraftFiles()) != 2 {
		t.Fatal("rollback lost original attachments")
	}
	if err := s.switchSelection(filepath.Join(root, "third", "draft-files.json"), false); err != nil || len(s.DraftFiles()) != 0 {
		t.Fatal("ordinary switch moved old attachments into an empty connection", err)
	}
}

func TestClipboardImageDraftRestoresPreviewsAndCleansAfterRemoval(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "draft-files.json")
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, pastePNG(t), nil }
	result, err := s.PasteAttachments()
	if err != nil || !result.Handled || len(result.Files) != 1 || !result.Files[0].Image || result.Files[0].Type != "image/png" {
		t.Fatalf("image paste: %+v %v", result, err)
	}
	jsonBytes, _ := json.Marshal(result)
	if strings.Contains(string(jsonBytes), root) {
		t.Fatal("host path escaped into renderer DTO")
	}
	imageURL, err := s.DraftImage(result.Files[0].ID)
	if err != nil || !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
		t.Fatalf("thumbnail: %v %q", err, imageURL)
	}
	path := s.files[0].path
	if _, err := os.Stat(path); err != nil {
		t.Fatal("clipboard image missing from private draft storage", err)
	}
	restarted, _, _ := setup()
	if err := restarted.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	if files := restarted.DraftFiles(); len(files) != 1 || files[0].ID != result.Files[0].ID || files[0].Unavailable {
		t.Fatalf("restart draft: %+v", files)
	}
	if _, err := restarted.RemoveFile(result.Files[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removed draft bytes remain: %v", err)
	}
}

func TestClipboardFileURLsTakePrecedenceAndBatchFailurePreservesDraft(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "draft-files.json")
	a, b := filepath.Join(root, "one.txt"), filepath.Join(root, "two.txt")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return []string{a, a, b}, pastePNG(t), nil }
	result, err := s.PasteAttachments()
	if err != nil || len(result.Files) != 2 || result.Files[0].Image || result.Files[1].Image {
		t.Fatalf("multiple clipboard representations imported twice: %+v %v", result, err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return []string{a, root}, nil, nil }
	if _, err := s.PasteAttachments(); err == nil || len(s.DraftFiles()) != 2 {
		t.Fatal("invalid Finder batch changed draft")
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, nil, nil }
	if empty, err := s.PasteAttachments(); err != nil || empty.Handled {
		t.Fatalf("plain-text clipboard intercepted: %+v %v", empty, err)
	}
}

func TestAcceptedClipboardImageDeletesBytesAndRestartRemovesOrphans(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "draft-files.json")
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, pastePNG(t), nil }
	result, err := s.PasteAttachments()
	if err != nil {
		t.Fatal(err)
	}
	path := s.files[0].path
	s.consumeDraftFiles([]string{result.Files[0].ID})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("accepted draft bytes remain: %v", err)
	}
	orphan := filepath.Join(root, "draft-images", "paste-orphan.png")
	if err := os.WriteFile(orphan, pastePNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := setup()
	if err := restarted.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan survived restart: %v", err)
	}
}

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
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
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

func TestAcceptedSelectionWriteFailureRetriesWithoutDeletingPastedBytes(t *testing.T) {
	root := t.TempDir()
	selection, blocked := filepath.Join(root, "draft-files.json"), filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, pastePNG(t), nil }
	first, err := s.PasteAttachments()
	if err != nil {
		t.Fatal(err)
	}
	path := s.files[0].path
	s.selectionFile = blocked
	if err := s.consumeDraftFilesChecked([]string{first.Files[0].ID}); err == nil {
		t.Fatal("failed selection write reported success")
	}
	if len(s.DraftFiles()) != 1 {
		t.Fatal("failed write erased selected metadata")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed write deleted pasted bytes", err)
	}
	s.selectionFile = selection
	if err := s.consumeDraftFilesChecked([]string{first.Files[0].ID}); err != nil {
		t.Fatal("recovered selection could not clear", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("consumed pasted bytes remain: %v", err)
	}
	restored, _, _ := setup()
	if err := restored.configureSelection(selection); err != nil || len(restored.DraftFiles()) != 0 {
		t.Fatalf("accepted file restored: %v %+v", err, restored.DraftFiles())
	}
}

func TestAcceptedPastedImageConsumesOnlyOriginalBatchAcrossRestart(t *testing.T) {
	root := t.TempDir()
	selection := filepath.Join(root, "draft-files.json")
	s, _, _ := setup()
	if err := s.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	s.readClipboard = func() ([]string, []byte, error) { return nil, pastePNG(t), nil }
	first, err := s.PasteAttachments()
	if err != nil {
		t.Fatal(err)
	}
	firstPath := s.files[0].path
	second, err := s.PasteAttachments()
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Files) != 2 {
		t.Fatalf("new image not staged: %+v", second.Files)
	}
	secondPath := s.files[1].path
	s.consumeDraftFiles([]string{first.Files[0].ID})
	s.consumeDraftFiles([]string{first.Files[0].ID}) // A second accepted observer is inert.
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("consumed image retained: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("newly staged image removed: %v", err)
	}
	restored, _, _ := setup()
	if err := restored.configureSelection(selection); err != nil {
		t.Fatal(err)
	}
	files := restored.DraftFiles()
	if len(files) != 1 || files[0].ID != second.Files[1].ID {
		t.Fatalf("restart lost new image or restored consumed one: %+v", files)
	}
	if _, err := restored.resolveDraftFiles([]string{first.Files[0].ID}); err == nil {
		t.Fatal("next send could repeat consumed image")
	}
}
