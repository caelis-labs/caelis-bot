//go:build webfixture

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// Same service/control/Runtime/storage wiring as the browser fixture; no UI
// shortcut assigns the displayed outcomes. Browser clicks are a separate gate.
func TestInteractionSubmissionOriginalReceiptAndReopen(t *testing.T) {
	root := t.TempDir()
	makeService := func() (*interactionEngine, *backend.Service, func()) {
		e := &engine{root: root, state: api.Snapshot{Connection: "ready", CanSend: true, Phase: "idle"}}
		if data, err := os.ReadFile(filepath.Join(root, "fixture-state.json")); err == nil {
			if json.Unmarshal(data, &e.state) != nil {
				t.Fatal("state")
			}
		}
		runtime := newInteractionEngine(e)
		service := backend.NewService(runtime, nil, nil, nil, nil)
		service.ConfigureChat(filepath.Join(root, "chat.sqlite"))
		return runtime, service, configureInteractions(runtime, service)
	}
	e, b, tick := makeService()
	await := func(check func(api.Snapshot) bool) {
		t.Helper()
		end := time.Now().Add(4 * time.Second)
		for time.Now().Before(end) {
			tick()
			if check(b.Snapshot()) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("projection did not converge")
	}
	await(func(s api.Snapshot) bool { return len(s.Items) >= 4 })
	cards := map[string]api.QuestionCard{}
	for _, item := range b.Snapshot().Items {
		for _, q := range item.QuestionCards {
			cards[q.ShortID] = q
		}
	}
	for _, tc := range []struct{ id, outcome string }{{"Q1", "accepted"}, {"Q2", "rejected"}, {"Q3", "unknown"}, {"Q4", "unknown"}} {
		q := cards[tc.id]
		if q.Binding == "" {
			t.Fatal("missing original card", tc.id)
		}
		b.AnswerQuestion(t.Context(), q.ShortID, q.Binding, "2", "answer-"+tc.id)
		await(func(s api.Snapshot) bool {
			for _, i := range s.Items {
				if i.RequestID == "answer-"+tc.id {
					return i.QuestionReply != nil && i.QuestionReply.Answer == "使用远端" && i.QuestionReply.Binding == q.Binding && i.QuestionReply.Outcome == tc.outcome && i.Text == "使用远端"
				}
			}
			return false
		})
	}
	// A late refusal belongs only to the original request and unlocks its card.
	data, _ := json.Marshal(map[string]string{"ID": "answer-Q4", "Outcome": "rejected"})
	if err := os.WriteFile(filepath.Join(root, "fixture-receipt-command.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	await(func(s api.Snapshot) bool {
		for _, i := range s.Items {
			for _, q := range i.QuestionCards {
				if q.ShortID == "Q4" {
					return q.State == "" && q.RejectedReplyID == "answer-Q4"
				}
			}
		}
		return false
	})
	q := cards["Q4"]
	b.AnswerQuestion(t.Context(), q.ShortID, q.Binding, "2", "answer-Q4-retry")
	await(func(s api.Snapshot) bool {
		for _, i := range s.Items {
			for _, q := range i.QuestionCards {
				if q.ShortID == "Q4" {
					return q.State == "" && q.RejectedReplyID == "answer-Q4-retry"
				}
			}
		}
		return false
	})
	var invalid api.DecisionValidationError
	if err := b.Decide(t.Context(), api.Decision{ID: "fixture-approval-accepted", Choice: "accept"}); !errors.As(err, &invalid) {
		t.Fatal("validation became uncertain", err)
	}
	if err := b.Decide(t.Context(), api.Decision{ID: "fixture-approval-accepted", Choice: "accept", Answers: map[string][]string{"value": {"valid"}}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Decide(t.Context(), api.Decision{ID: "fixture-approval-unknown", Choice: "accept", Answers: map[string][]string{"value": {"valid"}}}); err == nil {
		t.Fatal("unknown became success")
	}
	expectedReplies := map[string]struct{ question, outcome string }{
		"answer-Q1":       {"Q1", "accepted"},
		"answer-Q2":       {"Q2", "rejected"},
		"answer-Q3":       {"Q3", "unknown"},
		"answer-Q4":       {"Q4", "rejected"},
		"answer-Q4-retry": {"Q4", "rejected"},
	}
	repliesMatch := func(s api.Snapshot) bool {
		seen := map[string]bool{}
		for _, item := range s.Items {
			if item.Kind == "user" && (strings.Contains(item.Text, "/answer") || strings.Contains(item.Text, "send_user_message_question_reply")) {
				t.Fatal("native command leaked", item)
			}
			if item.QuestionReply == nil {
				continue
			}
			want, exists := expectedReplies[item.RequestID]
			if !exists || seen[item.RequestID] {
				t.Fatal("unexpected or duplicated reply identity", item.RequestID)
			}
			seen[item.RequestID] = true
			q := cards[want.question]
			reply := item.QuestionReply
			if reply.ShortID != q.ShortID || reply.Binding != q.Binding || reply.Title != q.Title || reply.Outcome != want.outcome || item.Status != want.outcome || item.Text != "使用远端" || reply.Answer != item.Text {
				return false
			}
		}
		return len(seen) == len(expectedReplies)
	}
	await(repliesMatch)
	if err := b.Shutdown(); err != nil {
		t.Fatal(err)
	}
	e, b, tick = makeService()
	defer b.Shutdown()
	await(repliesMatch)
	snap := b.Snapshot()
	if snap.Approvals[0].Status != "resolved" || snap.Approvals[0].Resolution.Outcome != "allowed" || snap.Approvals[1].Status != "unknown" {
		t.Fatal("approval receipt lost", snap.Approvals)
	}
	expectedEffects := map[string]struct{ kind, outcome string }{
		"answer-Q1":                 {"answer", "accepted"},
		"answer-Q2":                 {"answer", "rejected"},
		"answer-Q3":                 {"answer", "unknown"},
		"answer-Q4":                 {"answer", "rejected"},
		"answer-Q4-retry":           {"answer", "rejected"},
		"fixture-approval-accepted": {"approval", "allowed"},
		"fixture-approval-unknown":  {"approval", "unknown"},
	}
	assertEffects := func() int {
		t.Helper()
		e.mu.Lock()
		defer e.mu.Unlock()
		counts := map[string]int{}
		validations := 0
		for _, r := range e.receipts {
			if r.Kind == "validation" {
				validations++
				if r.ID != "fixture-approval-accepted" || r.Outcome != "not_dispatched" || r.Writes != 0 {
					t.Fatal("pre-dispatch refusal changed", r)
				}
				continue
			}
			want, exists := expectedEffects[r.ID]
			if !exists || r.Kind != want.kind || r.Outcome != want.outcome || r.Writes != 1 {
				t.Fatal("unexpected Runtime effect", r)
			}
			counts[r.ID]++
		}
		if len(counts) != len(expectedEffects) || validations != 1 {
			t.Fatalf("missing original effects or validation: effects=%v validations=%d", counts, validations)
		}
		for id, n := range counts {
			if n != 1 {
				t.Fatal("original effect replayed", id, n)
			}
		}
		return len(e.receipts)
	}
	beforeRetry := assertEffects()
	// Reopening must retain unknown authority even if a fresh client attempts
	// another answer with a different request ID.
	q = cards["Q3"]
	b.AnswerQuestion(t.Context(), q.ShortID, q.Binding, "2", "unknown-after-reopen")
	tick()
	if afterRetry := assertEffects(); afterRetry != beforeRetry {
		t.Fatalf("unknown retry reached Runtime: receipts %d -> %d", beforeRetry, afterRetry)
	}
	if !repliesMatch(b.Snapshot()) {
		t.Fatal("unknown retry changed the persisted original replies")
	}
	// Reopening and safe reads must not issue a new Runtime write.
	t.Log("production Service + controller + controlled Runtime original receipts + SQLite reopen; 5 answer writes, 2 approval writes, 1 pre-dispatch refusal")
}
