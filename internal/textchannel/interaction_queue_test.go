package textchannel

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTerminalWorkerQueueCompactsDuplicatePayloadAndDoesNotRevive(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	native := lifecycleQuestion()
	native.AsyncQuestions[0].Title = strings.Repeat("original long question ", 1000)
	card, err := s.AsyncWorkCard(native, "original-owner", "original-task")
	if err != nil {
		t.Fatal(err)
	}
	q := s.state.AsyncQuestions["Q1"]
	// Model the older on-disk format containing duplicate terminal payloads.
	q.State, q.Reason, q.Notified = "dismissed", "user_dismissed", true
	s.state.AsyncQuestions["Q1"] = q
	s.state.AsyncClosed[asyncKey(q)] = q
	old, _ := json.Marshal(s.state)
	if err = os.WriteFile(s.path, old, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.AsyncQuestions["Q1"].State != "dismissed" {
		t.Fatal("terminal identity lost")
	}
	retained := reopened.state.AsyncClosed[asyncKey(q)]
	if retained.Title != "" || len(retained.Options) != 0 || retained.Fingerprint != q.Fingerprint || retained.CallID != q.CallID || !retained.Notified {
		t.Fatal("terminal payload or original closure evidence changed", retained)
	}
	for range 3 {
		if _, err = reopened.AsyncWorkCard(native, "original-owner", "original-task"); err != nil {
			t.Fatal(err)
		}
	}
	if len(reopened.state.AsyncQuestions) != 1 || reopened.state.AsyncQuestions["Q1"].State != "dismissed" {
		t.Fatal("polling revived closed generation")
	}
	projected, _ := reopened.ProjectQuestions([]api.Item{card})
	if projected[0].QuestionCards[0].Title != q.Title || projected[0].QuestionCards[0].State != "dismissed" {
		t.Fatal("cleanup changed historical original")
	}
	// Reuse only the number, keeping the old generation's display and tombstone.
	reopened.state.NextQuestion = shortIDPoolSize
	fresh := native
	fresh.ID = "fresh"
	fresh.AsyncCallID = "fresh-call"
	if _, err = reopened.AsyncWorkCard(fresh, "original-owner", "original-task"); err != nil {
		t.Fatal(err)
	}
	oldCard, err := reopened.AsyncWorkCard(native, "original-owner", "original-task")
	if err != nil || !strings.Contains(oldCard.Text, q.Title) || oldCard.QuestionCards[0].State != "dismissed" {
		t.Fatal("number reuse lost original history or revived control", err)
	}
}
func TestUncertainWorkerQuestionPayloadSurvivesQueueCleanupAndRestart(t *testing.T) {
	for _, state := range []string{"unknown", "dispatching"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			s, _ := Open(root, nil)
			native := lifecycleQuestion()
			_, _ = s.AsyncWorkCard(native, "original-owner", "original-task")
			q := s.state.AsyncQuestions["Q1"]
			q.State, q.ReplyID = state, "original-answer-operation"
			s.state.AsyncQuestions["Q1"] = q
			s.state.AsyncClosed[asyncKey(q)] = q
			if err := s.save(); err != nil {
				t.Fatal(err)
			}
			s, err := Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.WorkerAsyncQuestions()) != 1 || s.state.AsyncClosed[asyncKey(q)].Title != q.Title || s.state.AsyncQuestions["Q1"].ReplyID != q.ReplyID {
				t.Fatal("uncertain original was pruned")
			}
		})
	}
}
