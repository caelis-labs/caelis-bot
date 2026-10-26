package textchannel

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestDesktopReplyPresentationUsesOriginalBindingAndReceipt(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := Open(t.TempDir(), nil)
			card, err := s.AsyncCard(api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "选择环境？", Options: []string{"保留本机", "使用远端"}}}}, "owner")
			if err != nil {
				t.Fatal(err)
			}
			var updates []api.Item
			s.SetQuestionReplyObserver(func(item api.Item) {
				if item.Kind == "user" {
					updates = append(updates, item)
				}
			})
			s.SetInboundObserver(func(Inbound) { t.Fatal("desktop button leaked control command") })
			calls := 0
			s.SetAsyncAnswerer(func(_ context.Context, in Inbound, model string) (api.Receipt, error) {
				calls++
				if in.Text != "/answer Q1 2" {
					t.Fatal("IM protocol changed", in.Text)
				}
				if len(updates) != 1 || updates[0].Status != "sending" {
					t.Fatal("missing dispatch presentation", updates)
				}
				return api.Receipt{ID: in.ID, Outcome: outcome}, nil
			})
			q := card.QuestionCards[0]
			snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
			s.HandleDesktopQuestion(t.Context(), "original-reply", q.ShortID, q.Binding, "2", snapshot)
			if len(updates) != 2 || updates[1].QuestionReply == nil {
				t.Fatal(updates)
			}
			reply := updates[1].QuestionReply
			if reply.Answer != "使用远端" || reply.Title != q.Title || reply.Binding != q.Binding || reply.ShortID != q.ShortID || reply.Outcome != outcome || updates[1].RequestID != "original-reply" {
				t.Fatalf("wrong projection: %+v", reply)
			}
			s.HandleDesktopQuestion(t.Context(), "original-reply", q.ShortID, q.Binding, "2", snapshot)
			if calls != 1 {
				t.Fatal("replayed original submission")
			}
			// A stale/recycled display handle must never label or submit a new answer.
			s.HandleDesktopQuestion(t.Context(), "stale-reply", q.ShortID, "different-binding", "1", snapshot)
			if len(updates) != 2 || calls != 1 {
				t.Fatal("stale card gained display or dispatch authority")
			}
		})
	}
}

func TestReplyProjectionReconcilesOnlyOriginalReceipt(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "继续？"}}}, "owner")
	var reply api.Item
	s.SetQuestionReplyObserver(func(item api.Item) {
		if item.Kind == "user" {
			reply = item
		}
	})
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	q := card.QuestionCards[0]
	snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
	s.HandleDesktopQuestion(t.Context(), "original", q.ShortID, q.Binding, "继续", snapshot)
	snapshot.LastReceipt = api.Receipt{ID: "other", Outcome: "accepted"}
	s.ReconcileAsync(snapshot, nil)
	projected, _ := s.ProjectQuestions([]api.Item{reply})
	if projected[0].QuestionReply.Outcome != "unknown" {
		t.Fatal("unrelated receipt confirmed reply")
	}
	snapshot.LastReceipt.ID = "original"
	s.ReconcileAsync(snapshot, nil)
	projected, _ = s.ProjectQuestions([]api.Item{reply})
	if projected[0].QuestionReply.Outcome != "accepted" {
		t.Fatal("original receipt not projected")
	}
}

func TestLateQuestionOutcomeNotifiesOnlyAfterOriginalReceipt(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			s, _ := Open(t.TempDir(), nil)
			card, _ := s.AsyncCard(api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "继续？"}}}, "owner")
			s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
				return api.Receipt{Outcome: "unknown"}, nil
			})
			q := card.QuestionCards[0]
			snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
			s.HandleDesktopQuestion(t.Context(), "original", q.ShortID, q.Binding, "继续", snapshot)
			notifications := 0
			s.SetQuestionReplyOutcomeObserver(func(request, binding, result string) {
				notifications++
				if request != "original" || binding != q.Binding || result != outcome {
					t.Fatalf("wrong original receipt: %q %q %q", request, binding, result)
				}
				// Also prove the callback is outside the control lock.
				s.Revision()
			})
			snapshot.LastReceipt = api.Receipt{ID: "other", Outcome: outcome}
			if err := s.ReconcileAsync(snapshot, nil); err != nil {
				t.Fatal(err)
			}
			if notifications != 0 {
				t.Fatal("unrelated receipt notification")
			}
			snapshot.LastReceipt.ID = "original"
			if err := s.ReconcileAsync(snapshot, nil); err != nil {
				t.Fatal(err)
			}
			if notifications != 1 {
				t.Fatal("missing original outcome")
			}
			if err := s.ReconcileAsync(snapshot, nil); err != nil {
				t.Fatal(err)
			}
			if notifications != 1 {
				t.Fatal("repeated outcome")
			}
		})
	}
}

func TestOriginalRejectedReplyIdentityUnlocksOnlyItsOwnQuestionAttempt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "Choose?", Options: []string{"A", "B"}}}}
	card, err := s.AsyncCard(item, "owner")
	if err != nil {
		t.Fatal(err)
	}
	q := card.QuestionCards[0]
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
	s.HandleDesktopQuestion(t.Context(), "original", q.ShortID, q.Binding, "2", snapshot)
	snapshot.LastReceipt = api.Receipt{ID: "unrelated", Outcome: "rejected"}
	if err = s.ReconcileAsync(snapshot, nil); err != nil {
		t.Fatal(err)
	}
	cards, _ := s.ProjectQuestions([]api.Item{card})
	if cards[0].QuestionCards[0].State != "unknown" || cards[0].QuestionCards[0].RejectedReplyID != "" {
		t.Fatal(cards)
	}
	snapshot.LastReceipt.ID = "original"
	if err = s.ReconcileAsync(snapshot, nil); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cards, _ = restored.ProjectQuestions([]api.Item{card})
	got := cards[0].QuestionCards[0]
	if got.State != "" || got.RejectedReplyID != "original" || got.Binding != q.Binding {
		t.Fatalf("wrong original rejection: %+v", got)
	}
}
