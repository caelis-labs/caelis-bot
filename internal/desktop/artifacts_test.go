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
