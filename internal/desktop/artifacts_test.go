package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactActionsUseOwnedHandleAndBoundedText(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "result.md")
	if err := os.WriteFile(source, []byte("# result\n可复制"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "saved.md")
	var opened, copied, suggested string
	s := &Service{
		resolveArtifact: func(id string) (string, error) {
			if id != "owned" {
				return "", errors.New("not owned")
			}
			return source, nil
		},
		openArtifactFile: func(path string) error { opened = path; return nil },
		saveArtifactPath: func(name string) (string, error) { suggested = name; return target, nil },
		copyText:         func(text string) bool { copied = text; return true },
	}
	if _, err := s.PreviewArtifact("unowned"); err == nil || s.OpenArtifact("unowned") == nil || s.CopyArtifactPath("unowned") == nil || s.SaveArtifactAs("unowned", "x") == nil {
		t.Fatal("unowned artifact reached an OS action")
	}
	if preview, err := s.PreviewArtifact("owned"); err != nil || preview.Text != "# result\n可复制" || preview.Truncated {
		t.Fatal(preview, err)
	}
	if err := s.CopyArtifactContent("owned"); err != nil || copied != "# result\n可复制" {
		t.Fatal("copy content", err)
	}
	if err := s.CopyArtifactPath("owned"); err != nil || copied != source {
		t.Fatal("copy path", err)
	}
	if err := s.OpenArtifact("owned"); err != nil || opened != source {
		t.Fatal("default open", err)
	}
	if err := s.SaveArtifactAs("owned", "../result.md"); err != nil || suggested != "result.md" {
		t.Fatal("save dialog", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "# result\n可复制" {
		t.Fatal("saved bytes", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewArtifact("owned"); err == nil || s.OpenArtifact("owned") == nil {
		t.Fatal("missing file was not reported")
	}
}

func TestArtifactPreviewWindowUsesOwnedFileAndSafeName(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.md")
	if err := os.WriteFile(path, []byte("# preview"), 0600); err != nil {
		t.Fatal(err)
	}
	shown := ""
	var s *Service
	s = &Service{
		resolveArtifact: func(id string) (string, error) {
			if id != "owned" {
				return "", errors.New("unowned")
			}
			return path, nil
		},
		openArtifactPreview: func() { selection, _ := s.CurrentArtifactPreview(); shown = selection.Name },
	}
	if err := s.ShowArtifactPreview("/private/tmp/arbitrary.md"); err == nil || shown != "" {
		t.Fatal("unowned path opened a preview")
	}
	if err := s.ShowArtifactPreview("owned"); err != nil || shown != "sample.md" {
		t.Fatal("owned preview", err, shown)
	}
	if selected, err := s.CurrentArtifactPreview(); err != nil || selected.ID != "owned" || selected.Name != "sample.md" {
		t.Fatal("preview selection", selected, err)
	}
	path = filepath.Join(root, "image.png")
	if err := os.WriteFile(path, []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.ShowArtifactPreview("owned"); err == nil || shown != "sample.md" {
		t.Fatal("nontext preview replaced the owned selection")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.ShowArtifactPreview("owned"); err == nil {
		t.Fatal("missing file opened a preview")
	}
}

func TestArtifactPreviewLatestSelectionWins(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"slow.md", "new.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	opened := make(chan string, 2)
	var s *Service
	s = &Service{
		resolveArtifact: func(id string) (string, error) {
			if id == "slow" {
				close(started)
				<-release
			}
			return filepath.Join(root, id+".md"), nil
		},
		openArtifactPreview: func() { selected, _ := s.CurrentArtifactPreview(); opened <- selected.ID },
	}
	done := make(chan error, 1)
	go func() { done <- s.ShowArtifactPreview("slow") }()
	<-started
	if err := s.ShowArtifactPreview("new"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if selected, err := s.CurrentArtifactPreview(); err != nil || selected.ID != "new" {
		t.Fatal("old preview replaced latest selection", selected, err)
	}
	if id := <-opened; id != "new" || len(opened) != 0 {
		t.Fatal("unexpected preview windows opened", id, len(opened))
	}
}

func TestArtifactPreviewLimitAndBinaryRejection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", artifactPreviewLimit+8)), 0600); err != nil {
		t.Fatal(err)
	}
	copied := ""
	s := &Service{resolveArtifact: func(string) (string, error) { return path, nil }, copyText: func(value string) bool { copied = value; return true }}
	preview, err := s.PreviewArtifact("owned")
	if err != nil || !preview.Truncated || len(preview.Text) != artifactPreviewLimit || s.CopyArtifactContent("owned") != nil || len(copied) != artifactPreviewLimit+8 {
		t.Fatal(preview.Truncated, len(preview.Text), err)
	}
	copied = ""
	if err := os.WriteFile(path, []byte(strings.Repeat("a", artifactCopyLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.CopyArtifactContent("owned"); err == nil || copied != "" {
		t.Fatal("oversized content copied")
	}
	if err := os.WriteFile(path, []byte{'a', 0, 'b'}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewArtifact("owned"); err == nil {
		t.Fatal("binary text preview accepted")
	}
}
