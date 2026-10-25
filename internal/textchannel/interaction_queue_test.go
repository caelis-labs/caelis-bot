package textchannel

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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

func TestTerminalHistoryPayloadRetentionKeepsCachedOriginalAndUncertainty(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	old := asyncQuestion{ShortID: "Q1", ItemID: "old-item", Fingerprint: "old-binding", State: "dismissed", Title: "Original old title", TaskTitle: "Original source task", Options: []string{"Original option"}, ClosedAt: time.Now().Add(-8 * 24 * time.Hour).Unix()}
	s.state.AsyncClosed[asyncKey(old)] = old
	for i := 0; i < 200; i++ {
		q := old
		q.ItemID = fmt.Sprint("history-", i)
		q.Fingerprint = fmt.Sprint("binding-", i)
		q.ClosedAt = time.Now().Unix()
		s.state.AsyncClosed[asyncKey(q)] = q
	}
	uncertain := old
	uncertain.Fingerprint = "unknown-original"
	uncertain.State = "unknown"
	uncertain.ReplyID = "original-receipt"
	s.state.AsyncClosed[asyncKey(uncertain)] = uncertain
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	full := 0
	for _, q := range s.state.AsyncClosed {
		if q.State != "unknown" && q.Title != "" {
			full++
		}
	}
	if full > 128 || s.state.AsyncClosed[asyncKey(old)].Title != "" || s.state.AsyncClosed[asyncKey(uncertain)].ReplyID != "original-receipt" || s.state.AsyncClosed[asyncKey(uncertain)].Title == "" {
		t.Fatal("retention changed receipt evidence or retained unbounded payloads")
	}
	card := api.Item{ID: old.ItemID, QuestionCards: []api.QuestionCard{{ShortID: old.ShortID, Binding: old.Fingerprint, Title: old.Title, TaskTitle: old.TaskTitle, Options: old.Options}}}
	result, _ := s.ProjectQuestions([]api.Item{card})
	if result[0].QuestionCards[0].Title != old.Title || result[0].QuestionCards[0].TaskTitle != old.TaskTitle || result[0].QuestionCards[0].State != "dismissed" {
		t.Fatal("compaction erased cached original")
	}
}

func TestCompactedNativeQuestionReplayPreservesOriginalWithoutRestoringPayload(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	original := lifecycleQuestion()
	card, err := s.AsyncCard(original, "owner")
	if err != nil {
		t.Fatal(err)
	}
	q := s.state.AsyncQuestions[card.QuestionCards[0].ShortID]
	q.State = "dismissed"
	q.ClosedAt = time.Now().Add(-8 * 24 * time.Hour).Unix()
	s.state.AsyncClosed[asyncKey(q)] = q
	delete(s.state.AsyncQuestions, q.ShortID)
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := s.AsyncCard(original, "owner")
	if err != nil || len(restored.QuestionCards) != 1 || restored.QuestionCards[0].Title != original.AsyncQuestions[0].Title || !strings.Contains(restored.Text, original.AsyncQuestions[0].Title) || restored.QuestionCards[0].State != "dismissed" {
		t.Fatal("native replay erased original display", err)
	}
	if s.state.AsyncClosed[asyncKey(q)].Title != "" || len(s.state.AsyncQuestions) != 0 {
		t.Fatal("native replay restored payload or active question")
	}
}

func TestRetiredSchemaReplayCannotOverwriteCompactedNewerOriginal(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	old := lifecycleQuestion()
	_, _ = s.AsyncCard(old, "owner")
	next := old
	next.AsyncQuestions = append([]api.AsyncQuestion(nil), old.AsyncQuestions...)
	next.AsyncQuestions[0].Title = "Newer exact original"
	latest, err := s.AsyncCard(next, "owner")
	if err != nil {
		t.Fatal(err)
	}
	q := s.state.AsyncQuestions[latest.QuestionCards[0].ShortID]
	q.State = "dismissed"
	q.ClosedAt = time.Now().Add(-8 * 24 * time.Hour).Unix()
	s.state.AsyncClosed[asyncKey(q)] = q
	delete(s.state.AsyncQuestions, q.ShortID)
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	replay, err := s.AsyncCard(old, "owner")
	if err != nil || replay.ID != "" {
		t.Fatal("retired schema could overwrite newer IM original", err)
	}
	projected, _ := s.ProjectQuestions([]api.Item{latest})
	if projected[0].QuestionCards[0].Title != "Newer exact original" || projected[0].QuestionCards[0].State != "dismissed" {
		t.Fatal("cached newer original lost")
	}
}
