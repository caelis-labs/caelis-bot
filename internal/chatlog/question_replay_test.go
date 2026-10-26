package chatlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Control writer scheduling to cover the gap between ingress and DB merge.
func controlledChatLog(path string) *Log {
	return &Log{path: path, dirty: map[string]api.Item{}, pendingInputs: map[string]api.Item{}, visibleLimit: pageSize, wake: make(chan struct{}, 1)}
}

func originalReply(outcome string) api.Item {
	return api.Item{ID: "control-input:original", Kind: "user", RequestID: "original", Text: "保留本机", Status: outcome, SeenAt: 123,
		QuestionReply: &api.QuestionReply{ShortID: "Q1", Binding: "original-binding", Title: "使用哪台机器？", Answer: "保留本机", Outcome: outcome}}
}

func assertReplyDisplay(t *testing.T, l *Log, outcome string, required bool) {
	t.Helper()
	items, _ := l.Snapshot()
	found := 0
	for _, item := range items {
		if strings.Contains(item.Text, "/answer") || strings.Contains(item.Text, "send_user_message_question_reply") {
			t.Fatal("unchecked native control body became visible")
		}
		if item.RequestID != "original" {
			continue
		}
		found++
		want := originalReply(outcome)
		if item.ID != want.ID || item.Text != want.Text || item.Status != outcome || item.SeenAt != want.SeenAt || item.QuestionReply == nil || *item.QuestionReply != *want.QuestionReply {
			t.Fatalf("original display or receipt changed: %+v", item)
		}
	}
	if found > 1 || required && found != 1 {
		t.Fatalf("original response appears %d times", found)
	}
}

func evictReply(t *testing.T, l *Log, prefix string) {
	t.Helper()
	for n := 0; n < pageSize+5; n++ {
		l.Observe([]api.Item{{ID: fmt.Sprintf("%s-%d", prefix, n), Kind: "assistant", Text: "later"}})
	}
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	items, earlier := l.Snapshot()
	if !earlier || len(items) != pageSize {
		t.Fatal("fixture did not evict historical response")
	}
	for _, item := range items {
		if item.RequestID == "original" {
			t.Fatal("original response remained in the visible cache")
		}
	}
}

func TestQuestionReplySurvivesEvictionReplayAndReopen(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chat.sqlite")
			l := controlledChatLog(path)
			l.Observe([]api.Item{originalReply(outcome)})
			if err := l.flush(t.Context()); err != nil {
				t.Fatal(err)
			}
			evictReply(t, l, "first")
			l.Observe([]api.Item{{ID: "native-echo", RequestID: "original", Kind: "user", Text: "/answer Q1 1", Status: "completed"}})
			assertReplyDisplay(t, l, outcome, false)
			if err := l.flush(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertReplyDisplay(t, l, outcome, true)
			if outcome != "unknown" {
				evictReply(t, l, "second")
				stale := originalReply("sending")
				stale.Text, stale.QuestionReply.Title, stale.QuestionReply.Answer = "stale", "stale", "stale"
				l.Observe([]api.Item{stale, {ID: "echo-again", RequestID: "original", Kind: "user", Text: "<send_user_message_question_reply>internal</send_user_message_question_reply>", Status: "completed"}})
				assertReplyDisplay(t, l, outcome, false)
				if err := l.flush(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertReplyDisplay(t, l, outcome, true)
			}
			reopened := Open(path)
			defer reopened.Close()
			eventually(t, func() bool { reopened.mu.Lock(); defer reopened.mu.Unlock(); return reopened.loaded })
			for {
				_, earlier := reopened.Snapshot()
				if !earlier {
					break
				}
				if err := reopened.LoadEarlier(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			assertReplyDisplay(t, reopened, outcome, true)
		})
	}
}

func TestQueuedReplyAndEchoKeepReceiptAcrossReadFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "chat.sqlite")
	l := controlledChatLog(path)
	l.Observe([]api.Item{originalReply("unknown")})
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	evictReply(t, l, "later")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	l.Observe([]api.Item{originalReply("unknown"), {ID: "echo", RequestID: "original", Kind: "user", Text: "/answer Q1 1", Status: "completed"}})
	l.ResolveQuestionReply("original", "original-binding", "accepted")
	if err := l.flush(t.Context()); err == nil {
		t.Fatal("expected blocked database read")
	}
	assertReplyDisplay(t, l, "accepted", false)
	if len(l.pendingInputs) != 1 {
		t.Fatal("failed read discarded the incoming response")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertReplyDisplay(t, l, "accepted", true)
}

func TestQueuedReplyMergesNewReceiptWhileOriginalReadWaits(t *testing.T) {
	l := controlledChatLog(filepath.Join(t.TempDir(), "chat.sqlite"))
	l.Observe([]api.Item{originalReply("unknown")})
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	evictReply(t, l, "later")
	l.Observe([]api.Item{originalReply("sending")})
	db, err := l.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connection, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	done := make(chan error, 1)
	go func() { done <- l.resolvePendingInputs(t.Context(), db) }()
	eventually(t, func() bool { return db.Stats().WaitCount > 0 })
	l.Observe([]api.Item{originalReply("accepted"), {ID: "native", Kind: "user", RequestID: "original", Text: "/answer Q1 1", Status: "completed"}})
	l.ResolveQuestionReply("original", "original-binding", "accepted")
	assertReplyDisplay(t, l, "accepted", false)
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertReplyDisplay(t, l, "accepted", true)
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestReplyMergeRetainsLiveDisplayWhenWriteFails(t *testing.T) {
	l := controlledChatLog(filepath.Join(t.TempDir(), "chat.sqlite"))
	l.Observe([]api.Item{originalReply("rejected")})
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	evictReply(t, l, "later")
	db, err := l.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER blocked_write BEFORE UPDATE ON messages BEGIN SELECT RAISE(FAIL, 'synthetic write failure'); END`); err != nil {
		t.Fatal(err)
	}
	l.Observe([]api.Item{originalReply("unknown"), {ID: "native", Kind: "user", RequestID: "original", Text: "/answer Q1 1", Status: "completed"}})
	if err := l.flush(t.Context()); err == nil {
		t.Fatal("fixture write unexpectedly succeeded")
	}
	assertReplyDisplay(t, l, "rejected", true)
	if len(l.dirty) == 0 {
		t.Fatal("failed persistence discarded the merged display")
	}
	if _, err := db.Exec(`DROP TRIGGER blocked_write`); err != nil {
		t.Fatal(err)
	}
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertReplyDisplay(t, l, "rejected", true)
}

func TestQueuedInputsPreserveObservationOrderAndControllerFields(t *testing.T) {
	l := controlledChatLog(filepath.Join(t.TempDir(), "chat.sqlite"))
	l.Observe([]api.Item{originalReply("unknown"), {ID: "following", Kind: "assistant", Text: "following"}, {ID: "native", Kind: "user", RequestID: "original", Text: "/answer Q1 1", Status: "completed"}})
	assertReplyDisplay(t, l, "unknown", false)
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	items, _ := l.Snapshot()
	if len(items) != 2 || items[0].ID != "control-input:original" || items[1].ID != "following" {
		t.Fatal("asynchronous merge reordered the original response")
	}
	assertReplyDisplay(t, l, "unknown", true)
}

func TestQueuedReplyRetainsReceiptBeforeItsFirstDatabaseInsert(t *testing.T) {
	l := controlledChatLog(filepath.Join(t.TempDir(), "chat.sqlite"))
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	l.Observe([]api.Item{originalReply("unknown")})
	l.ResolveQuestionReply("original", "original-binding", "accepted")
	// An older flush may have no row to patch yet. The pending display must
	// already carry the exact receipt independently of that transient patch.
	l.questionOutcomes = nil
	l.Observe([]api.Item{{ID: "native", Kind: "user", RequestID: "original", Text: "/answer Q1 1", Status: "completed"}})
	if err := l.flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertReplyDisplay(t, l, "accepted", true)
}
