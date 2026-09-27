package backend

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type screenEngine struct {
	api.Engine
	state       string
	submissions int
	input       api.Submission
	files       []api.InputFile
	outcome     string
}

func (e *screenEngine) Snapshot() api.Snapshot { return api.Snapshot{} }
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
