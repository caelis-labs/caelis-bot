package backend

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestChatAnswersUseUserSubmissionAndPreserveMatchingComposerDraft(t *testing.T) {
	const text = "发布前小测\n\n哪个版本可以发布？\n通过验收的版本\n\n填写待验证的平台\nWindows"
	engine := &imReceiptEngine{value: api.Snapshot{CanSend: true}}
	s := NewService(engine, nil, nil, nil, nil)
	s.ConfigureChat(filepath.Join(t.TempDir(), "chat.sqlite"))
	defer s.chat.Close()
	saved, err := s.SaveDraft(api.Draft{Text: text})
	if err != nil {
		t.Fatal(err)
	}
	s.SetCommandHandler(func(context.Context, api.Submission) (api.Receipt, bool, error) {
		t.Fatal("a form title or answer reached the chat command parser")
		return api.Receipt{}, false, nil
	})
	s.SetUserSubmitter(func(ctx context.Context, input api.Submission, files []api.InputFile) (api.Receipt, error) {
		if input.Text != text || input.Quoted != nil || len(input.FileIDs) != 0 || len(input.ReferenceIDs) != 0 || len(files) != 0 {
			t.Fatalf("ordinary text input changed: %+v", input)
		}
		return engine.Submit(ctx, input, files)
	})
	receipt, err := s.SubmitChatAnswers(t.Context(), "form-original", text)
	if err != nil || receipt.ID != "form-original" || receipt.Outcome != "accepted" {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if got := s.Draft(); got.Text != saved.Text || got.Revision != saved.Revision {
		t.Fatalf("form cleared an identical composer draft: %+v", got)
	}
	s.ObserveChat(api.Snapshot{Items: []api.Item{{ID: "native-input", RequestID: receipt.ID, Kind: "user", Text: text, Status: "completed"}}})
	items := s.ChatSnapshot(0, "", "").Snapshot.Items
	if len(items) != 1 || items[0].RequestID != receipt.ID || items[0].Kind != "user" || items[0].Text != text {
		t.Fatalf("ordinary chat message did not reconcile: %+v", items)
	}
}

func TestChatAnswersKeepUncertaintyAndRecognizePreDispatchRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		cause      error
	}{
		{"recovery refusal", "rejected", api.ErrRecoveryPending},
		{"uncertain transport", "", errors.New("connection ended")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &imReceiptEngine{}
			s := NewService(engine, nil, nil, nil, nil)
			draft, err := s.SaveDraft(api.Draft{Text: "unsent composer draft"})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.SetUserSubmitter(func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
				calls++
				return api.Receipt{}, tc.cause
			})
			receipt, err := s.SubmitChatAnswers(t.Context(), "form-original", "题目\n答案")
			if receipt.Outcome != tc.want || tc.want == "" && err == nil || tc.want != "" && (err != nil || receipt.ID != "form-original") {
				t.Fatalf("original receipt = %+v, %v", receipt, err)
			}
			if got := s.Draft(); calls != 1 || got.Text != draft.Text || got.Revision != draft.Revision {
				t.Fatalf("calls=%d, draft=%+v", calls, got)
			}
		})
	}
}
