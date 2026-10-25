package textchannel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestExpiredAnswerPersistsAndPublishesClosureBeforeReturning(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	q := s.state.AsyncQuestions["Q1"]
	q.ExpiresAt = 1
	s.state.AsyncQuestions["Q1"] = q
	calls, updates := 0, 0
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		calls++
		return api.Receipt{Outcome: "accepted"}, nil
	})
	s.SetAsyncUpdateObserver(func(item api.Item) {
		updates++
		if len(item.QuestionCards) != 1 || item.QuestionCards[0].State != "expired" || !strings.Contains(item.Text, "已过期") {
			t.Error("closure update missing expired card or text")
		}
		// Reading/reopening here also proves the observer is outside the lock,
		// and publication follows durable commit rather than optimistic display.
		projected, _ := s.ProjectQuestions([]api.Item{card})
		disk, err := Open(root, nil)
		if err != nil || disk.state.AsyncQuestions["Q1"].State != "expired" || projected[0].QuestionCards[0].State != "expired" {
			t.Error("expiry not durable before publication")
		}
	})
	for _, id := range []string{"first-attempt", "second-attempt"} {
		got := s.HandleDesktopQuestion(t.Context(), id, "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{RuntimeOwner: "owner", Connection: "ready"})
		if !strings.Contains(got, "已过期") {
			t.Fatal("expired input did not return expired feedback")
		}
	}
	if calls != 0 || updates != 1 || len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("expired input dispatched, repeated its update, or lost closure")
	}
}

func TestAnswerExpirySaveFailureDoesNotPublishClosureOrDispatch(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	q := s.state.AsyncQuestions["Q1"]
	q.ExpiresAt = 1
	s.state.AsyncQuestions["Q1"] = q
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(blocked, "ledger")
	s.SetAsyncUpdateObserver(func(api.Item) { t.Error("uncommitted expiry published") })
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		t.Error("expiry save failure dispatched an answer")
		return api.Receipt{}, nil
	})
	got := s.HandleDesktopQuestion(t.Context(), "failed-save", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	if !strings.Contains(got, "未提交") || s.state.AsyncQuestions["Q1"].State != "" || len(s.state.AsyncClosed) != 0 {
		t.Fatal("failed expiry save claimed a durable closure")
	}
}

func TestExpiryRacingAnswerPreservesOriginalDispatchAndReceipt(t *testing.T) {
	for _, outcome := range []string{"accepted", "unknown", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			root := t.TempDir()
			s, _ := Open(root, nil)
			card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan string, 1)
			calls := 0
			s.SetAsyncAnswerer(func(_ context.Context, in Inbound, _ string) (api.Receipt, error) {
				calls++
				if in.ID != "original-answer" {
					t.Error("original answer identity changed")
				}
				close(entered)
				<-release
				return api.Receipt{ID: in.ID, Outcome: outcome}, nil
			})
			snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
			go func() {
				done <- s.HandleDesktopQuestion(t.Context(), "original-answer", "Q1", card.QuestionCards[0].Binding, "1", snapshot)
			}()
			<-entered
			s.mu.Lock()
			q := s.state.AsyncQuestions["Q1"]
			q.ExpiresAt = 1
			s.state.AsyncQuestions["Q1"] = q
			s.mu.Unlock()
			_ = s.ReconcileAsync(api.Snapshot{}, nil)
			competing := s.HandleDesktopQuestion(t.Context(), "competing-answer", "Q1", card.QuestionCards[0].Binding, "2", snapshot)
			if !strings.Contains(competing, "待核对") {
				t.Error("expiry obscured an in-flight dispatch")
			}
			close(release)
			feedback := <-done
			want := map[string]string{"accepted": "answered", "unknown": "unknown", "rejected": "expired"}[outcome]
			_ = s.ReconcileAsync(api.Snapshot{}, nil)
			q = s.state.AsyncQuestions["Q1"]
			if q.State != want || calls != 1 || outcome != "rejected" && q.ReplyID != "original-answer" {
				t.Fatal("expiry replaced the original receipt or duplicated dispatch")
			}
			if outcome == "rejected" && (!strings.Contains(feedback, "已过期") || strings.Contains(feedback, "重新回答")) {
				t.Fatal("rejected expired answer offered resubmission")
			}
			s, _ = Open(root, nil)
			if s.state.AsyncQuestions["Q1"].State != want {
				t.Fatal("settled outcome did not survive restart")
			}
		})
	}
}

func TestRejectedExpiredAnswerSaveFailureKeepsOriginalClaimFenced(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.SetAsyncAnswerer(func(_ context.Context, in Inbound, _ string) (api.Receipt, error) {
		calls++
		s.mu.Lock()
		q := s.state.AsyncQuestions["Q1"]
		q.ExpiresAt = 1
		s.state.AsyncQuestions["Q1"] = q
		s.path = filepath.Join(blocked, "ledger")
		s.mu.Unlock()
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, nil
	})
	s.SetAsyncUpdateObserver(func(item api.Item) {
		if item.QuestionCards[0].State != "unknown" {
			t.Error("uncommitted closure published")
		}
	})
	snapshot := api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}
	_ = s.HandleDesktopQuestion(t.Context(), "original-claim", "Q1", card.QuestionCards[0].Binding, "1", snapshot)
	_ = s.HandleDesktopQuestion(t.Context(), "competing", "Q1", card.QuestionCards[0].Binding, "1", snapshot)
	q := s.state.AsyncQuestions["Q1"]
	if calls != 1 || q.State != "unknown" || q.ReplyID != "original-claim" || len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("save failure lost the original fence or claimed closure")
	}
}
