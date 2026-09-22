package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type draftEngine struct {
	snapshotEngine
	outcome string
}

func (e draftEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	return api.Receipt{ID: in.ID, Outcome: e.outcome}, nil
}
func TestDurableDraftRestoresAndOnlyAcceptedSubmissionClears(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "draft.json")
			create := func() *Service {
				s := NewService(draftEngine{outcome: outcome}, func([]string) ([]api.InputFile, error) { return nil, nil }, func([]string) {}, nil, nil)
				if err := s.ConfigureDraft(path); err != nil {
					t.Fatal(err)
				}
				return s
			}
			s := create()
			if _, err := s.SaveDraft(api.Draft{Text: "中文草稿\n第二行", ReferenceIDs: []string{"stable-reference"}}); err != nil {
				t.Fatal(err)
			}
			restored := create()
			d := restored.Draft()
			if d.Text != "中文草稿\n第二行" || len(d.ReferenceIDs) != 1 || d.Revision != 1 {
				t.Fatal("draft not restored")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("draft not private", err)
			}
			if _, err := restored.Submit(context.Background(), api.Submission{ID: "attempt", Text: d.Text, ReferenceIDs: d.ReferenceIDs}); err != nil {
				t.Fatal(err)
			}
			next := create().Draft()
			if (next.Text == "") != (outcome == "accepted") {
				t.Fatal("draft cleared without acceptance or resurrected", outcome)
			}
		})
	}
}
func TestCorruptDraftAndWriteFailureDoNotReplaceStoredContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	if s.ConfigureDraft(path) == nil || s.Draft().Notice == "" {
		t.Fatal("corrupt draft hidden")
	}
	if _, err := s.SaveDraft(api.Draft{Text: "replacement"}); err == nil {
		t.Fatal("corrupt draft overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "incomplete" {
		t.Fatal("changed corrupt file")
	}
	s = NewService(snapshotEngine{}, nil, nil, nil, nil)
	s.draftFile = t.TempDir() // Atomic rename cannot replace a directory.
	if _, err := s.SaveDraft(api.Draft{Text: "not persisted"}); err == nil || s.Draft().Text != "" {
		t.Fatal("reported unsaved draft as saved")
	}
}
func TestMessageLinksOnlyOpenExplicitWebURLs(t *testing.T) {
	opened := 0
	s := NewService(snapshotEngine{}, nil, nil, func(string) error { opened++; return nil }, nil)
	for _, url := range []string{"javascript:alert(1)", "file:///tmp/private", "data:text/html,test", "//example.com", "https://user:secret@example.com"} {
		if s.OpenMessageLink(url) == nil {
			t.Fatal("unsafe link accepted", url)
		}
	}
	if opened != 0 || s.OpenMessageLink("https://example.com/path?q=test") != nil || opened != 1 {
		t.Fatal("explicit web link failed")
	}
}
