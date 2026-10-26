package textchannel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func lifecycleQuestion() api.Item {
	return api.Item{ID: "question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "How?", Options: []string{"One", "Two"}}}}
}
func TestDismissSurvivesRestartStaleIMAndNumberReuse(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	native := lifecycleQuestion()
	card, _ := s.AsyncCard(native, "owner")
	if card.QuestionCards[0].ExpiresAt-time.Now().Unix() < 86390 {
		t.Fatal("missing default deadline")
	}
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		t.Fatal("dismiss sent an answer")
		return api.Receipt{}, nil
	})
	if got := s.DismissQuestion(t.Context(), "dismiss", "Q1", card.QuestionCards[0].Binding); !strings.Contains(got, "已关闭") {
		t.Fatal(got)
	}
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	projected, _ := s.ProjectQuestions([]api.Item{card})
	if projected[0].QuestionCards[0].State != "dismissed" {
		t.Fatal(projected)
	}
	if got := s.HandleDesktopQuestion(t.Context(), "late", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{RuntimeOwner: "owner", Connection: "ready"}); !strings.Contains(got, "已关闭") {
		t.Fatal(got)
	}
	s.state.NextQuestion = shortIDPoolSize
	fresh := native
	fresh.ID = "new-question"
	fresh.AsyncCallID = "new-call"
	next, _ := s.AsyncCard(fresh, "owner")
	if next.QuestionCards[0].ShortID != "Q1" {
		t.Fatal(next)
	}
	projected, _ = s.ProjectQuestions([]api.Item{card})
	if projected[0].QuestionCards[0].State != "dismissed" {
		t.Fatal("recycled number lost original close", projected)
	}
	if got := s.DismissQuestion(t.Context(), "stale", "Q1", card.QuestionCards[0].Binding); !strings.Contains(got, "失效") {
		t.Fatal(got)
	}
	replay, _ := s.AsyncCard(native, "owner")
	if len(replay.QuestionCards) != 1 || replay.QuestionCards[0].State != "dismissed" {
		t.Fatal("replay revived closed question")
	}
}
func TestExpiryRunsOfflineWithoutNativePermissionDecision(t *testing.T) {
	decisions := 0
	s, _ := Open(t.TempDir(), func(context.Context, api.Decision) error { decisions++; return nil })
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	q := s.state.AsyncQuestions["Q1"]
	q.ExpiresAt = time.Now().Add(-time.Second).Unix()
	s.state.AsyncQuestions["Q1"] = q
	_, _, _ = s.Card(api.Approval{ID: "approval", Status: "pending", Choices: []api.Choice{{ID: "allow", Label: "Allow"}}})
	if err := s.Reconcile(api.Snapshot{Connection: "offline"}); err != nil {
		t.Fatal(err)
	}
	projected, _ := s.ProjectQuestions([]api.Item{card})
	if projected[0].QuestionCards[0].State != "expired" || decisions != 0 || len(s.state.Prompts) != 1 {
		t.Fatal(projected, decisions, s.state.Prompts)
	}
	if len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("closure unexpectedly queued Worker input")
	}
}
func TestQuestionDismissRacesAnswerAndCannotEraseUnknown(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan string, 1)
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		close(entered)
		<-release
		return api.Receipt{Outcome: "unknown"}, nil
	})
	go func() {
		done <- s.HandleDesktopQuestion(t.Context(), "answer", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{RuntimeOwner: "owner", Connection: "ready"})
	}()
	<-entered
	if got := s.DismissQuestion(t.Context(), "dismiss", "Q1", card.QuestionCards[0].Binding); !strings.Contains(got, "待核对") {
		t.Fatal(got)
	}
	close(release)
	<-done
	q := s.state.AsyncQuestions["Q1"]
	q.ExpiresAt = 1
	s.state.AsyncQuestions["Q1"] = q
	_ = s.ReconcileAsync(api.Snapshot{}, nil)
	if s.state.AsyncQuestions["Q1"].State != "unknown" || len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("unknown answer erased")
	}
}
func TestWorkerLifecycleYieldFinalCancelAndReplacement(t *testing.T) {
	for _, test := range []struct {
		status, result string
		stop           bool
		want           string
	}{{"completed", "", false, ""}, {"completed", "Result", false, ""}, {"interrupted", "", false, ""}, {"unavailable", "", false, ""}, {"working", "", true, "expired"}, {"unknown", "", false, ""}} {
		t.Run(test.status+test.result, func(t *testing.T) {
			s, _ := Open(t.TempDir(), nil)
			_, _ = s.AsyncWorkCard(lifecycleQuestion(), "codex", "work")
			_ = s.ReconcileAsync(api.Snapshot{}, []api.WorkState{{Runtime: "codex", Task: api.Task{ID: "work", Title: "Source", Status: test.status, Result: test.result}, StopRequested: test.stop}})
			q := s.state.AsyncQuestions["Q1"]
			if q.State != test.want || q.State == "" && q.TaskTitle != "Source" {
				t.Fatal(q)
			}
		})
	}
	s, _ := Open(t.TempDir(), nil)
	old := lifecycleQuestion()
	_, _ = s.AsyncCard(old, "owner")
	changed := lifecycleQuestion()
	changed.AsyncQuestions[0].Options = []string{"New"}
	card, _ := s.AsyncCard(changed, "owner")
	if s.state.AsyncQuestions["Q1"].State != "superseded" {
		t.Fatal("not superseded")
	}
	_, _ = s.AsyncCard(old, "owner")
	if s.state.AsyncActive[old.ID] != card.QuestionCards[0].Binding {
		t.Fatal("historical replay displaced current question")
	}
}
func TestDismissSaveFailureDoesNotClaimClosure(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	blocked := filepath.Join(root, "blocked")
	_ = os.WriteFile(blocked, []byte("file"), 0600)
	s.path = filepath.Join(blocked, "ledger")
	if got := s.DismissQuestion(t.Context(), "dismiss", "Q1", card.QuestionCards[0].Binding); !strings.Contains(got, "未关闭") {
		t.Fatal(got)
	}
	if s.state.AsyncQuestions["Q1"].State != "" || len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("failed close escaped")
	}
}

func TestQuestionPresentationPreservesOriginalAnswerAndRestart(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	item := lifecycleQuestion()
	item.AsyncQuestions[0].Title = "Which isolated model connection should the demo video use?"
	item.AsyncQuestions[0].Options = []string{"Sign in manually in the isolated demo Bot", "Use an existing independent demo connection; I will provide its location"}
	_, _ = s.AsyncWorkCard(item, "codex", "work")
	summary := &api.QuestionPresentation{Title: item.AsyncQuestions[0].Title, Options: append([]string(nil), item.AsyncQuestions[0].Options...)}
	presented, err := s.PresentWorkerQuestion("Q1", summary)
	if err != nil || presented.QuestionCards[0].Options[1] != summary.Options[1] {
		t.Fatal(presented, err)
	}
	s, err = Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	projected, _ := s.ProjectQuestions([]api.Item{presented})
	if projected[0].QuestionCards[0].Context != summary.Context {
		t.Fatal(projected)
	}
	bad := *summary
	bad.Options = []string{"Other", "Different"}
	if _, err = s.PresentWorkerQuestion("Q1", &bad); err == nil {
		t.Fatal("relabelled a visible question")
	}
	var delivered string
	s.SetAsyncWorkAnswerer(func(_ context.Context, _ Inbound, work, owner, id, input string) (api.Receipt, error) {
		if work != "work" || owner != "codex" || id != item.ID {
			t.Fatal("lost original owner")
		}
		delivered = input
		return api.Receipt{Outcome: "accepted"}, nil
	})
	answer := s.HandleDesktopQuestion(t.Context(), "answer-original", "Q1", presented.QuestionCards[0].Binding, "2", api.Snapshot{})
	if !strings.Contains(answer, "已提交") || !strings.Contains(delivered, item.AsyncQuestions[0].Options[1]) {
		t.Fatal(answer, delivered)
	}
}
func TestPresentationRejectsParaphrasesOrChangedOptions(t *testing.T) {
	for _, summary := range []api.QuestionPresentation{{Title: "Choose", Options: []string{"Same", "same"}}, {Title: "Choose", Options: []string{"One"}}, {Title: "Different question", Options: []string{"One", "Two"}}, {Title: "Choose", Context: "Added condition", Options: []string{"One", "Two"}}} {
		if validateQuestionPresentation(summary, "Choose", []string{"One", "Two"}) == nil {
			t.Fatal("accepted invalid presentation", summary)
		}
	}
}
func TestExpiredSlotArchivesClosureBeforeNumberReuse(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	card, _ := s.AsyncCard(lifecycleQuestion(), "owner")
	q := s.state.AsyncQuestions["Q1"]
	q.ExpiresAt = 1
	s.state.AsyncQuestions["Q1"] = q
	s.state.NextQuestion = shortIDPoolSize
	fresh := lifecycleQuestion()
	fresh.ID = "fresh"
	fresh.AsyncCallID = "fresh-call"
	if _, err := s.AsyncCard(fresh, "owner"); err != nil {
		t.Fatal(err)
	}
	old, _ := s.ProjectQuestions([]api.Item{card})
	if old[0].QuestionCards[0].State != "expired" || len(s.PendingQuestionClosures()) != 0 {
		t.Fatal("expiry lost while reusing number", old)
	}
}

func TestAnsweringPresentedFieldDoesNotRevealPrivateSiblings(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := lifecycleQuestion()
	item.AsyncQuestions = append(item.AsyncQuestions, api.AsyncQuestion{Title: "Private Worker context", Options: []string{"A", "B"}})
	_, _ = s.AsyncWorkCard(item, "codex", "work")
	card, err := s.PresentWorkerQuestion("Q1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var updated api.Item
	s.SetAsyncUpdateObserver(func(item api.Item) { updated = item })
	s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "accepted"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "answer-field", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	if len(updated.QuestionCards) != 1 || updated.QuestionCards[0].ShortID != "Q1" || strings.Contains(updated.Text, "Private") || strings.Contains(card.Text, "Private") {
		t.Fatal("unpresented field leaked", updated)
	}
	if s.state.AsyncQuestions["Q2"].State != "" {
		t.Fatal("unanswered sibling expired")
	}
}

func TestCompletedQuestionTurnWithCommentaryStillAwaitsInput(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	_, _ = s.AsyncWorkCard(lifecycleQuestion(), "codex", "work")
	state := api.WorkState{Runtime: "codex", TurnKey: "turn", Task: api.Task{ID: "work", Status: "completed", Result: "I prepared the assets; waiting for the model connection."}}
	if err := s.ReconcileAsync(api.Snapshot{}, []api.WorkState{state}); err != nil {
		t.Fatal(err)
	}
	if s.state.AsyncQuestions["Q1"].State != "" {
		t.Fatal("commentary was mistaken for task completion")
	}
	state.TurnKey = "later-completed-turn"
	state.Task.Result = "Delivered"
	state.TaskEnded = true // Only an explicit task-level fact closes the question.
	if err := s.ReconcileAsync(api.Snapshot{}, []api.WorkState{state}); err != nil {
		t.Fatal(err)
	}
	if s.state.AsyncQuestions["Q1"].Reason != "task_completed" {
		t.Fatal("explicit task termination failed to expire pending question")
	}
}

func TestOldSchemaReplayCannotEraseTheNewLiveCard(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	old := lifecycleQuestion()
	_, _ = s.AsyncCard(old, "owner")
	next := lifecycleQuestion()
	next.AsyncQuestions = []api.AsyncQuestion{{Title: "Which new connection?", Options: []string{"A", "B"}}}
	current, _ := s.AsyncCard(next, "owner")
	replay, _ := s.AsyncCard(old, "owner")
	if len(replay.QuestionCards) != 1 || replay.QuestionCards[0].Binding != current.QuestionCards[0].Binding || replay.QuestionCards[0].Title != next.AsyncQuestions[0].Title || replay.QuestionCards[0].State != "" {
		t.Fatal("late old item erased the live card", replay)
	}
}

func TestDelayedWorkerObservationSharesOriginalDeadline(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := lifecycleQuestion()
	deadline := time.Now().Add(4 * time.Hour).Unix() // Source first observed it 20h ago.
	item.AsyncExpiresAt = deadline
	card, err := s.AsyncWorkCard(item, "original-owner", "original-task")
	if err != nil || card.QuestionCards[0].ExpiresAt != deadline {
		t.Fatal("late observation extended native deadline", err)
	}
	if _, err = s.AsyncWorkCard(item, "original-owner", "original-task"); err != nil || s.state.AsyncQuestions["Q1"].ExpiresAt != deadline {
		t.Fatal("replay extended deadline", err)
	}
	// An already expired source cannot flash a fresh actionable card.
	old := item
	old.ID = "expired-original"
	old.AsyncCallID = "expired-call"
	old.AsyncExpiresAt = time.Now().Add(-time.Second).Unix()
	closed, err := s.AsyncWorkCard(old, "original-owner", "original-task")
	if err != nil || len(closed.QuestionCards) > 0 && closed.QuestionCards[0].State != "expired" {
		t.Fatal("late source revived an expired question", err)
	}
}
