package localstate_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

func TestAnswerSaveDeadlineRequiresOriginalReceiptWithoutReplay(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			root := t.TempDir()
			s, err := textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			item := api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "Choose", Options: []string{"One", "Two"}}}}
			card, err := s.AsyncCard(item, "owner")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var release func()
			var receipt api.Receipt
			s.SetAsyncAnswerer(func(_ context.Context, in textchannel.Inbound, _ string) (api.Receipt, error) {
				calls++
				release = localstate.HoldWriterForTest(filepath.Join(root, "text-channel-control.json"))
				receipt = api.Receipt{ID: in.ID, Outcome: outcome}
				return receipt, nil
			})
			observedUnknown := false
			s.SetAsyncUpdateObserver(func(update api.Item) {
				if release == nil {
					return
				}
				// This observer runs after the original post-answer Store.save failed,
				// before reply persistence. No retry can turn that failure into success.
				observedUnknown = len(update.QuestionCards) == 1 && update.QuestionCards[0].State == "unknown"
				release()
				release = nil
			})
			t.Cleanup(func() {
				if release != nil {
					release()
				}
			})
			snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
			s.HandleDesktopQuestion(t.Context(), "original-input", "Q1", card.QuestionCards[0].Binding, "1", snapshot)
			if !observedUnknown || calls != 1 || receipt.ID != "original-input" {
				t.Fatal("save deadline was treated as completion")
			}
			s, err = textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			cards, _ := s.ProjectQuestions([]api.Item{card})
			if cards[0].QuestionCards[0].State != "unknown" || cards[0].QuestionCards[0].Binding != card.QuestionCards[0].Binding {
				t.Fatal("restart lost original uncertainty")
			}
			s.SetAsyncAnswerer(func(context.Context, textchannel.Inbound, string) (api.Receipt, error) {
				t.Fatal("replayed native answer")
				return api.Receipt{}, nil
			})
			s.HandleDesktopQuestion(t.Context(), "competing-input", "Q1", card.QuestionCards[0].Binding, "2", snapshot)
			snapshot.LastReceipt = api.Receipt{ID: "unrelated-input", Outcome: outcome}
			if err = s.ReconcileAsync(snapshot, nil); err != nil {
				t.Fatal(err)
			}
			cards, _ = s.ProjectQuestions([]api.Item{card})
			if cards[0].QuestionCards[0].State != "unknown" {
				t.Fatal("unrelated receipt settled answer")
			}
			snapshot.LastReceipt = receipt
			if err = s.ReconcileAsync(snapshot, nil); err != nil {
				t.Fatal(err)
			}
			s, err = textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := s.AsyncCard(item, "owner")
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if outcome == "accepted" {
				want = "answered"
			}
			if len(restored.QuestionCards) != 1 || restored.QuestionCards[0].State != want || restored.QuestionCards[0].Binding != card.QuestionCards[0].Binding || calls != 1 {
				t.Fatal("original receipt did not settle exactly once")
			}
		})
	}
}
