package textchannel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestV0183RedactedProductionMigrationAndRestart(t *testing.T) {
	raw, err := os.ReadFile("testdata/v0183-control-redacted.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Control    document
		Provenance struct{ CapturedUTC string }
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	captured, err := time.Parse(time.RFC3339Nano, fixture.Provenance.CapturedUTC)
	if err != nil {
		t.Fatal(err)
	}
	shift := time.Now().Unix() - captured.Unix()
	closed, pending := 0, 0
	for _, records := range []map[string]asyncQuestion{fixture.Control.AsyncQuestions, fixture.Control.AsyncClosed} {
		for id, q := range records {
			for _, stamp := range []*int64{&q.IssuedAt, &q.ExpiresAt, &q.ClosedAt} {
				if *stamp > 0 {
					*stamp += shift
				}
			}
			records[id] = q
		}
	}
	for _, q := range fixture.Control.AsyncQuestions {
		if closedAsync(q.State) || q.State == "answered" {
			closed++
		}
		if q.State == "" && !asyncExpired(q) {
			pending++
		}
	}
	if closed < 21 || fixture.Control.AsyncQuestions["Q16"].State != "unknown" || fixture.Control.AsyncQuestions["Q17"].State != "unknown" {
		t.Fatal("fixture no longer represents production defect")
	}
	root := t.TempDir()
	path := filepath.Join(root, "text-channel-control.json")
	encoded, _ := json.Marshal(fixture.Control)
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		s, err := Open(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		gotPending := 0
		for _, q := range s.state.AsyncQuestions {
			if q.State == "" {
				if !asyncExpired(q) {
					gotPending++
				}
				continue
			}
			if q.Title != "" || len(q.Options) > 0 || q.Presentation != nil || q.TaskTitle != "" {
				t.Fatal("closed prose survived migration")
			}
		}
		if gotPending != pending || len(s.state.AsyncReceipts) != 2 {
			t.Fatal("migration lost valid pending or uncertain receipts", gotPending, pending)
		}
		for _, id := range []string{"Q16", "Q17"} {
			q := s.state.AsyncQuestions[id]
			r := s.state.AsyncReceipts[asyncKey(q)]
			if r.ReplyID != fixture.Control.AsyncQuestions[id].ReplyID || r.WorkID != q.WorkID || r.CallID != q.CallID || r.State != "unknown" {
				t.Fatal("original uncertain receipt changed")
			}
		}
		if len(s.PendingQuestionClosures()) != 0 {
			t.Fatal("migration injected model input")
		}
	}
}

func TestLongRunningQuestionLedgerBoundedAcrossRestart(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	// Advance through many completed generations, keeping the source's immutable
	// deadline expired before collection. Only the 999-number display ring remains.
	for n := 0; n < 12000; n++ {
		id := fmt.Sprintf("Q%d", n%shortIDPoolSize+1)
		q := asyncQuestion{ShortID: id, ItemID: fmt.Sprint("item-", n), Fingerprint: fmt.Sprint("binding-", n), State: "answered", ReplyID: fmt.Sprint("original-receipt-", n), Title: strings.Repeat("temporary prose", 100), IssuedAt: time.Now().Add(-48 * time.Hour).Unix(), ExpiresAt: time.Now().Add(-24 * time.Hour).Unix()}
		s.state.AsyncQuestions[id] = q
		s.state.AsyncClosed[asyncKey(q)] = q
		s.state.AsyncActive[q.ItemID] = q.Fingerprint
		s.state.AsyncRetired[q.Fingerprint] = true
		s.compactClosedQuestionCopies()
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.state.AsyncQuestions) > shortIDPoolSize || len(s.state.AsyncClosed) > 0 || len(s.state.AsyncActive) > shortIDPoolSize || len(s.state.AsyncRetired) > shortIDPoolSize {
		t.Fatal("unbounded control index")
	}
	old := lifecycleQuestion()
	old.ID = "item-0"
	old.AsyncExpiresAt = -1
	replay, err := s.AsyncCard(old, "original-owner")
	if err != nil || len(replay.QuestionCards) > 0 {
		t.Fatal("collected historical source was renewed", err)
	}
	b, _ := os.ReadFile(s.path)
	if len(b) > 800<<10 || strings.Contains(string(b), "temporary prose") {
		t.Fatal("temporary payload accumulated", len(b))
	}
}

func TestResolvedUnknownNativeClaimAndOrphanDraftReclaimed(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	a := api.Approval{ID: "original", Owner: "task", TurnKey: "original-turn", Status: "pending", Choices: []api.Choice{{ID: "accept", Label: "Allow"}}}
	_, id, err := s.Card(a)
	if err != nil {
		t.Fatal(err)
	}
	p := s.state.Prompts[id]
	s.state.Claims[a.ID] = claim{State: "unknown"}
	s.state.Answers[answerKey(a.ID, p.Schema)] = map[string][]string{"field": {"private draft"}}
	a.Status = "resolved"
	if err := s.Reconcile(api.Snapshot{Connection: "ready", Approvals: []api.Approval{a}}); err != nil {
		t.Fatal(err)
	}
	if len(s.state.Prompts) != 0 || len(s.state.Answers) != 0 || s.state.Claims[a.ID].State != "resolved" {
		t.Fatal("exact resolution did not reclaim controls")
	}
	if err := s.Reconcile(api.Snapshot{Connection: "offline"}); err != nil {
		t.Fatal(err)
	}
	s, err = Open(filepath.Dir(s.path), nil)
	if err != nil || len(s.state.Answers) != 0 {
		t.Fatal("restart restored draft", err)
	}
}

func TestRealAnswerReceiptsSettleRecycleAndRestartWithoutAccumulation(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	calls := 0
	accepted := true
	bind := func() {
		s.SetAsyncAnswerer(func(_ context.Context, _ Inbound, _ string) (api.Receipt, error) {
			calls++
			if accepted {
				return api.Receipt{Outcome: "accepted"}, nil
			}
			return api.Receipt{Outcome: "rejected"}, nil
		})
	}
	bind()
	for n := 0; n < 257; n++ {
		s.state.NextQuestion = shortIDPoolSize
		item := lifecycleQuestion()
		item.ID = fmt.Sprint("real-item-", n)
		item.AsyncCallID = fmt.Sprint("real-call-", n)
		item.AsyncExpiresAt = time.Now().Add(time.Hour).Unix()
		card, err := s.AsyncCard(item, "owner")
		if err != nil {
			t.Fatal(err)
		}
		accepted = n%2 == 0
		feedback := s.HandleDesktopQuestion(t.Context(), fmt.Sprint("original-dispatch-", n), "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{Connection: "ready", RuntimeOwner: "owner"})
		q := s.state.AsyncQuestions["Q1"]
		if len(s.state.AsyncReceipts) != 0 || accepted && q.State != "answered" || !accepted && (q.State != "" || q.Title != item.AsyncQuestions[0].Title) {
			t.Fatal("settled receipt leaked or rejected answer lost its original", n, feedback)
		}
		q.ExpiresAt = time.Now().Add(-time.Second).Unix()
		s.state.AsyncQuestions["Q1"] = q
		if _, ok := s.state.AsyncClosed[asyncKey(q)]; ok {
			s.state.AsyncClosed[asyncKey(q)] = q
		}
		if err := s.ReconcileAsync(api.Snapshot{}, nil); err != nil {
			t.Fatal(err)
		}
		if n%100 == 0 {
			s, err = Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			bind()
		}
	}
	if calls != 257 || len(s.state.AsyncReceipts) != 0 || len(s.state.AsyncClosed) > 1 || len(s.state.AsyncActive) > 1 {
		t.Fatal("real dispatch history accumulated", calls, len(s.state.AsyncActive))
	}
}

func TestUnadmittedOldSchemaCannotRetireCurrentQuestion(t *testing.T) {
	for _, deadline := range []int64{-1, time.Now().Add(-time.Hour).Unix()} {
		s, _ := Open(t.TempDir(), nil)
		current := lifecycleQuestion()
		current.AsyncQuestions[0].Title = "Current exact original"
		card, err := s.AsyncCard(current, "owner")
		if err != nil {
			t.Fatal(err)
		}
		old := current
		old.AsyncQuestions = []api.AsyncQuestion{{Title: "Old schema"}}
		old.AsyncExpiresAt = deadline
		rejected, err := s.AsyncCard(old, "owner")
		if err != nil || rejected.ID != "" {
			t.Fatal("unadmitted schema was projected")
		}
		got, _ := s.ProjectQuestions([]api.Item{card})
		if got[0].QuestionCards[0].State != "" || got[0].QuestionCards[0].Title != current.AsyncQuestions[0].Title || s.state.AsyncActive[current.ID] != card.QuestionCards[0].Binding {
			t.Fatal("old schema retired current question")
		}
	}
}

func TestOriginalReceiptRejectionRestoresExactSourceBeforeAnswer(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	item := lifecycleQuestion()
	item.AsyncExpiresAt = time.Now().Add(time.Hour).Unix()
	card, _ := s.AsyncWorkCard(item, "codex", "original-work")
	s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "original-answer", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	q := s.state.AsyncQuestions["Q1"]
	operation := q.OperationID
	deadline := q.ExpiresAt
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SettleAsyncOperation("original-work", "different-operation", "rejected"); err != nil || s.state.AsyncQuestions["Q1"].State != "unknown" {
		t.Fatal("foreign receipt changed original")
	}
	if err := s.SettleAsyncOperation("original-work", operation, "rejected"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	input := ""
	s.SetAsyncWorkAnswerer(func(_ context.Context, _ Inbound, _, _, _, body string) (api.Receipt, error) {
		calls++
		input = body
		return api.Receipt{Outcome: "accepted"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "premature-answer", "Q1", card.QuestionCards[0].Binding, "2", api.Snapshot{})
	if calls != 0 {
		t.Fatal("missing source sent numeric text without original options")
	}
	if _, err := s.AsyncWorkCard(item, "codex", "original-work"); err != nil {
		t.Fatal(err)
	}
	s.HandleDesktopQuestion(t.Context(), "after-source-answer", "Q1", card.QuestionCards[0].Binding, "2", api.Snapshot{})
	if calls != 1 || !strings.Contains(input, `"answer":"Two"`) || !strings.Contains(input, `"question":"How?"`) || s.state.AsyncQuestions["Q1"].ExpiresAt != deadline || len(s.state.AsyncReceipts) != 0 {
		t.Fatal("rejected receipt lost exact source or renewed deadline")
	}
}

func TestNativeControlsCachesStayBoundedWithRetiredSecretPrivacy(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	for n := 0; n < 8000; n++ {
		key := fmt.Sprint("reply-", n)
		s.state.Replies[key] = "sanitized feedback"
		s.state.ReplyOrder = append(s.state.ReplyOrder, key)
		s.state.ReplyTimes[key] = int64(n)
		s.state.Claims[key] = claim{State: "submitted", IssuedAt: time.Now().Unix()}
		s.state.Cards[key] = cardBinding{NativeID: key, Secret: n%2 == 0, ShortID: "Q1", IssuedAt: time.Now().Add(-8 * 24 * time.Hour).Unix()}
		s.state.Answers[key] = map[string][]string{"field": {"discarded draft"}}
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Dir(s.path), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.state.Replies) > controlHistoryLimit || len(s.state.Claims) > controlHistoryLimit || len(s.state.Cards) > controlHistoryLimit || len(s.state.Answers) != 0 || len(s.state.RetiredSecretCards) != retiredCardPrivacyBytes {
		t.Fatal("unbounded approval history or orphan drafts")
	}
	if !s.retiredSecretCard("reply-0") {
		t.Fatal("retired privacy marker lost")
	}
}

func TestCrashDispatchingBecomesUnknownThenOriginalProofSettles(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	item := lifecycleQuestion()
	_, _ = s.AsyncWorkCard(item, "codex", "original-work")
	q := s.state.AsyncQuestions["Q1"]
	q.State, q.ReplyID, q.OperationID = "dispatching", "original-ingress", "original-native-operation"
	s.state.AsyncQuestions["Q1"] = q
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.state.AsyncQuestions["Q1"].State != "unknown" || len(s.AsyncAnswerReceipts()) != 1 {
		t.Fatal("crash dispatch was left in flight or reset unsent")
	}
	if err := s.SettleAsyncOperation("original-work", "original-native-operation", "accepted"); err != nil {
		t.Fatal(err)
	}
	if s.state.AsyncQuestions["Q1"].State != "answered" || len(s.state.AsyncReceipts) != 0 {
		t.Fatal("original acceptance did not release uncertain slot")
	}
	s.state.NextQuestion = shortIDPoolSize
	item.ID = "new-native-item"
	item.AsyncCallID = "new-native-call"
	card, err := s.AsyncWorkCard(item, "codex", "original-work")
	if err != nil || card.QuestionCards[0].ShortID != "Q1" {
		t.Fatal("settled original still occupied its slot", err)
	}
}

func TestFinishedUncertainQuestionCannotReopenAfterLaterRejectedReceipt(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := lifecycleQuestion()
	card, _ := s.AsyncWorkCard(item, "codex", "work")
	s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "original-answer", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	operation := s.state.AsyncQuestions["Q1"].OperationID
	if err := s.ReconcileAsync(api.Snapshot{}, []api.WorkState{{Runtime: "codex", Task: api.Task{ID: "work"}, TaskEnded: true}}); err != nil {
		t.Fatal(err)
	}
	q := s.state.AsyncQuestions["Q1"]
	if q.State != "unknown" || q.Reason != "task_completed" {
		t.Fatal("finish settled uncertain effect or lost presentation closure")
	}
	if err := s.SettleAsyncOperation("work", operation, "rejected"); err != nil {
		t.Fatal(err)
	}
	_, _ = s.AsyncWorkCard(item, "codex", "work")
	q = s.state.AsyncQuestions["Q1"]
	if q.State != "expired" || q.Reason != "task_completed" || len(s.state.AsyncReceipts) != 0 {
		t.Fatal("later receipt reopened a finished task question")
	}
}

func TestInFlightRejectionCannotUndoTaskClosure(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncWorkCard(lifecycleQuestion(), "codex", "work")
	entered, release := make(chan struct{}), make(chan struct{})
	s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
		close(entered)
		<-release
		return api.Receipt{Outcome: "rejected"}, nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.HandleDesktopQuestion(t.Context(), "original-input", "Q1", card.QuestionCards[0].Binding, "2", api.Snapshot{})
	}()
	<-entered
	if err := s.ReconcileAsync(api.Snapshot{}, []api.WorkState{{Runtime: "codex", Task: api.Task{ID: "work"}, TaskEnded: true}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if q := s.state.AsyncQuestions["Q1"]; q.State != "expired" || q.Reason != "task_completed" {
		t.Fatal("rejected callback undid task closure")
	}
}

func TestResidentRejectedReceiptCannotUndoOwnerClosure(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "original-owner")
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "original-input", "Q1", card.QuestionCards[0].Binding, "2", api.Snapshot{RuntimeOwner: "original-owner", Connection: "ready"})
	if err := s.ReconcileAsync(api.Snapshot{RuntimeOwner: "new-owner", Connection: "ready"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileAsync(api.Snapshot{RuntimeOwner: "original-owner", Connection: "ready", LastReceipt: api.Receipt{ID: "original-input", Outcome: "rejected"}}, nil); err != nil {
		t.Fatal(err)
	}
	if q := s.state.AsyncQuestions["Q1"]; q.State != "superseded" || q.Reason != "owner_replaced" {
		t.Fatal("rejected resident receipt undid owner closure")
	}
}
