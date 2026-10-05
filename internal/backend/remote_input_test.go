package backend

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRemoteInputUsesResidentAdmissionAndPreservesDesktopDraft(t *testing.T) {
	consumed := 0
	calls := 0
	s := NewService(snapshotEngine{}, func([]string) ([]api.InputFile, error) { t.Fatal("remote accessed desktop selection"); return nil, nil }, func([]string) { consumed++ }, nil, nil)
	s.SaveDraft(api.Draft{Text: "unfinished desktop draft"})
	s.SetUserSubmitter(func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		calls++
		if in.Text != "phone input" || len(files) != 1 {
			t.Fatal("remote submission changed")
		}
		return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
	})
	receipt, e := SubmitRemote(t.Context(), s, api.Submission{ID: "phone-1", Text: "phone input"}, []api.InputFile{{Name: "photo.jpg", Path: "native-owned-path"}})
	if e != nil || receipt.Outcome != "accepted" || calls != 1 || consumed != 0 || s.Draft().Text != "unfinished desktop draft" || s.pendingDraft != nil {
		t.Fatal("second client altered composer or bypassed resident")
	}
	s.restarting = true
	if _, e = SubmitRemote(t.Context(), s, api.Submission{ID: "phone-2"}, nil); e == nil || calls != 1 {
		t.Fatal("remote bypassed runtime restart fence")
	}
}
