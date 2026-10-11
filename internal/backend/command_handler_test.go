package backend

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestDesktopControlCommandBypassesOrdinarySubmitGate(t *testing.T) {
	s := NewService(snapshotEngine{}, nil, nil, nil, nil)
	if err := s.ConfigureDraft(filepath.Join(t.TempDir(), "draft.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDraft(api.Draft{Text: "/answer Q1 2"}); err != nil {
		t.Fatal(err)
	}
	before := s.Draft().Revision
	s.restarting = true
	called := 0
	s.SetCommandHandler(func(_ context.Context, in api.Submission) (api.Receipt, bool, error) {
		if in.Text != "/answer Q1 2" {
			return api.Receipt{}, false, nil
		}
		called++
		if in.ID != "desktop-command" || in.Text != "/answer Q1 2" {
			t.Fatalf("command changed: %+v", in)
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted", Message: "决定已提交"}, true, nil
	})
	receipt, err := s.Submit(t.Context(), api.Submission{ID: "desktop-command", Text: "/answer Q1 2"})
	if err != nil || receipt.Outcome != "accepted" || called != 1 {
		t.Fatalf("command blocked by ordinary submission gate: %+v %v", receipt, err)
	}
	if draft := s.Draft(); draft.Text != "" || draft.Revision <= before || draft.Notice != "" {
		t.Fatalf("accepted command left desktop draft unsettled: %+v", draft)
	}
	if _, err := s.Submit(t.Context(), api.Submission{ID: "ordinary-input", Text: "hello"}); err == nil {
		t.Fatal("ordinary input bypassed restart gate")
	}
}
