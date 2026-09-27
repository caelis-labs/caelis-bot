package backend

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type screenEngine struct {
	api.Engine
	snapshot    api.Snapshot
	state       string
	submissions int
	input       api.Submission
	files       []api.InputFile
	outcome     string
}

func (e *screenEngine) Snapshot() api.Snapshot { return e.snapshot }
func (e *screenEngine) ImageInput(context.Context) (api.ImageInputCapability, error) {
	return api.ImageInputCapability{State: e.state}, nil
}
func (e *screenEngine) Submit(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	e.submissions++
	e.input = in
	e.files = files
	return api.Receipt{ID: in.ID, Outcome: e.outcome}, nil
}
func TestSubmitScreenGatesCurrentModelAndPreservesComposer(t *testing.T) {
	for _, state := range []string{"unknown", "unsupported", "supported"} {
		for _, outcome := range []string{"accepted", "rejected", "unknown"} {
			t.Run(state+"/"+outcome, func(t *testing.T) {
				e := &screenEngine{state: state, outcome: outcome}
				consumed := false
				s := NewService(e, nil, func([]string) { consumed = true }, nil, nil)
				draft, err := s.SaveDraft(api.Draft{Text: "unfinished ordinary prompt", ReferenceIDs: []string{"ref-one"}})
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := SubmitScreen(t.Context(), s, api.Submission{ID: "screen-test", Text: "screen input", FileIDs: []string{"wrong"}, ReferenceIDs: []string{"wrong"}}, []api.InputFile{{Path: "/owned/selection.png", Name: "selection.png"}})
				if state != "supported" {
					if err == nil || receipt.Outcome != "rejected" || e.submissions != 0 {
						t.Fatal("unsupported model accepted", receipt, err)
					}
				} else if err != nil || receipt.Outcome != outcome || e.submissions != 1 || !e.input.ScreenInput || len(e.input.FileIDs) > 0 || len(e.input.ReferenceIDs) > 0 || len(e.files) != 1 {
					t.Fatal("screen route failed", receipt, err)
				}
				if s.Draft().Text != draft.Text || s.Draft().Revision != draft.Revision || consumed || s.pendingDraft != nil {
					t.Fatal("capture consumed ordinary draft")
				}
			})
		}
	}
}

func TestScreenMediaRestoresPresentationWithoutChangingRuntime(t *testing.T) {
	e := &screenEngine{state: "supported", outcome: "accepted"}
	s := NewService(e, nil, nil, nil, nil)
	store := filepath.Join(t.TempDir(), "ScreenMedia")
	if err := ConfigureScreenMedia(s, store); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "selection.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	raw := screeninput.Prompt(screeninput.Snapshot{Version: 1, ID: "capture-test", Source: "screen", Application: "Example browser", Note: "Translate"})
	input := api.Submission{ID: "screen-test-restore", Text: raw}
	if r, err := SubmitScreen(t.Context(), s, input, []api.InputFile{{Path: path, Name: "selection.png"}}); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	e.snapshot = api.Snapshot{Items: []api.Item{{Kind: "user", RequestID: input.ID, Text: raw}}}
	restored := NewService(e, nil, nil, nil, nil)
	if err = ConfigureScreenMedia(restored, store); err != nil {
		t.Fatal(err)
	}
	view := restored.Snapshot().Items[0]
	if view.Text != "Translate" || view.Screen == nil || view.Screen.Application != "Example browser" || len(view.Screen.Images) != 1 {
		t.Fatal(view)
	}
	if url, err := restored.ScreenImage(view.Screen.Images[0].ID, true); err != nil || !strings.HasPrefix(url, "data:image/jpeg;") {
		t.Fatal(err)
	}
	if ScreenSnapshot(restored).Items[0].Text != raw || e.input.Text != raw {
		t.Fatal("presentation changed model input")
	}
}
func TestScreenMediaFailureRejectsBeforeDispatch(t *testing.T) {
	e := &screenEngine{state: "supported", outcome: "accepted"}
	s := NewService(e, nil, nil, nil, nil)
	if err := ConfigureScreenMedia(s, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r, err := SubmitScreen(t.Context(), s, api.Submission{ID: "screen-missing"}, []api.InputFile{{Path: "/missing/capture.png"}})
	if err == nil || r.Outcome != "rejected" || e.submissions != 0 {
		t.Fatal("sent without retained media", r, err)
	}
}
