package textchannel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestAnswerSaveFailureRetainsOriginalReceiptAcrossRestart(t *testing.T) {
	for _, worker := range []bool{false, true} {
		for _, outcome := range []string{"accepted", "rejected"} {
			name := outcome + "/resident"
			if worker {
				name = outcome + "/worker"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				s, err := Open(root, nil)
				if err != nil {
					t.Fatal(err)
				}
				item := lifecycleQuestion()
				var card api.Item
				if worker {
					card, err = s.AsyncWorkCard(item, "owner", "work")
				} else {
					card, err = s.AsyncCard(item, "owner")
				}
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				// The original dispatch claim remains writable. Only the post-answer save
				// fails, using a real invalid replacement target rather than a fake result.
				blocked := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(blocked, []byte("block parent directory"), 0600); err != nil {
					t.Fatal(err)
				}
				answer := func(in Inbound) (api.Receipt, error) {
					calls++
					s.mu.Lock()
					s.path = filepath.Join(blocked, "ledger")
					s.mu.Unlock()
					return api.Receipt{ID: in.ID, Outcome: outcome}, nil
				}
				s.SetAsyncAnswerer(func(_ context.Context, in Inbound, _ string) (api.Receipt, error) { return answer(in) })
				s.SetAsyncWorkAnswerer(func(_ context.Context, in Inbound, work, owner, id, _ string) (api.Receipt, error) {
					if work != "work" || owner != "owner" || id != item.ID {
						t.Fatal("changed native target")
					}
					return answer(in)
				})
				snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
				_ = s.HandleDesktopQuestion(t.Context(), "original-input", "Q1", card.QuestionCards[0].Binding, "1", snapshot)
				original := s.state.AsyncQuestions["Q1"]
				failure := localstate.DescribeFailure(s.lastSaveError)
				if calls != 1 || original.State != "unknown" || original.ReplyID != "original-input" || original.CallID != item.AsyncCallID || s.lastSaveError == nil {
					t.Fatalf("lost uncertain original: calls=%d state=%s save=%+v", calls, original.State, failure)
				}
				if worker && original.OperationID == "" {
					t.Fatal("missing original Worker operation")
				}
				s, err = Open(root, nil)
				if err != nil {
					t.Fatal(err)
				}
				restored := s.state.AsyncQuestions["Q1"]
				if restored.State != "unknown" || restored.ReplyID != original.ReplyID || restored.OperationID != original.OperationID || restored.Fingerprint != original.Fingerprint || restored.Owner != original.Owner || restored.CallID != original.CallID {
					t.Fatal("restart lost original uncertain identity")
				}
				s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
					t.Fatal("replayed resident answer")
					return api.Receipt{}, nil
				})
				s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
					t.Fatal("replayed Worker answer")
					return api.Receipt{}, nil
				})
				_ = s.HandleDesktopQuestion(t.Context(), "competing-input", "Q1", card.QuestionCards[0].Binding, "2", snapshot)
				if worker {
					err = s.SettleAsyncOperation("work", "unrelated-operation", outcome)
				} else {
					snapshot.LastReceipt = api.Receipt{ID: "unrelated-input", Outcome: outcome}
					err = s.ReconcileAsync(snapshot, nil)
				}
				if err != nil || s.state.AsyncQuestions["Q1"].State != "unknown" {
					t.Fatal("unrelated receipt settled uncertainty", err)
				}
				if worker {
					err = s.SettleAsyncOperation("work", original.OperationID, outcome)
				} else {
					snapshot.LastReceipt.ID = original.ReplyID
					err = s.ReconcileAsync(snapshot, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				s, err = Open(filepath.Dir(s.path), nil)
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if outcome == "accepted" {
					want = "answered"
				}
				if s.state.AsyncQuestions["Q1"].State != want || len(s.state.AsyncReceipts) != 0 || calls != 1 {
					t.Fatal("original receipt did not durably settle once")
				}
			})
		}
	}
}
