package textchannel

import (
	"context"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestReviewLaterCommentaryMustNotExpireUnansweredSibling(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := lifecycleQuestion()
	item.AsyncQuestions = append(item.AsyncQuestions, api.AsyncQuestion{Title: "Which demo connection?", Options: []string{"Manual login", "Existing isolated connection"}})
	card, _ := s.AsyncWorkCard(item, "codex", "work")
	s.SetAsyncWorkAnswerer(func(context.Context, Inbound, string, string, string, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "accepted"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "answer-field-one", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	// Codex projects every non-async agentMessage into Result, including commentary.
	next := api.WorkState{Runtime: "codex", TurnKey: "answer-continuation", Task: api.Task{ID: "work", Status: "completed", Result: "The first choice is recorded; I still need your demo connection answer."}}
	if err := s.ReconcileAsync(api.Snapshot{}, []api.WorkState{next}); err != nil {
		t.Fatal(err)
	}
	if q := s.state.AsyncQuestions["Q2"]; q.State != "" {
		t.Fatalf("unanswered sibling closed after ordinary continuation: state=%q reason=%q", q.State, q.Reason)
	}
}

func TestReviewPresentationMustNotReverseChoiceMeaning(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	item := lifecycleQuestion()
	item.AsyncQuestions[0] = api.AsyncQuestion{Title: "May I publish the demo publicly?", Options: []string{"Publish publicly to the production website", "Keep local; do not publish"}}
	_, _ = s.AsyncWorkCard(item, "codex", "work")
	card, err := s.PresentWorkerQuestion("Q1", &api.QuestionPresentation{Title: "Publish the demo?", Options: []string{"Keep local", "Publish publicly"}})
	if err != nil {
		return
	}
	var delivered string
	s.SetAsyncWorkAnswerer(func(_ context.Context, _ Inbound, _, _, _, input string) (api.Receipt, error) {
		delivered = input
		return api.Receipt{Outcome: "accepted"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "visible-local-choice", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{})
	if strings.Contains(delivered, "Publish publicly to the production website") {
		t.Fatalf("visible choice %q submitted hidden opposite original option: %s", card.QuestionCards[0].Options[0], delivered)
	}
}

func TestReviewUnknownOldSchemaReplayMustKeepNewQuestion(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	old := lifecycleQuestion()
	card, _ := s.AsyncCard(old, "owner")
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "unknown"}, nil
	})
	s.HandleDesktopQuestion(t.Context(), "uncertain-answer", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{RuntimeOwner: "owner", Connection: "ready"})
	next := lifecycleQuestion()
	next.AsyncQuestions = []api.AsyncQuestion{{Title: "Updated question", Options: []string{"New one", "New two"}}}
	current, _ := s.AsyncCard(next, "owner")
	replay, _ := s.AsyncCard(old, "owner")
	if len(replay.QuestionCards) != 1 || replay.QuestionCards[0].Binding != current.QuestionCards[0].Binding || replay.QuestionCards[0].State != "" {
		t.Fatalf("stale unknown generation displaced the pending replacement: %#v; replacement state=%q", replay.QuestionCards, s.state.AsyncQuestions[current.QuestionCards[0].ShortID].State)
	}
}

func TestRetiredUnknownReceiptSurvivesRestartAndLateDispatch(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	old := lifecycleQuestion()
	card, _ := s.AsyncCard(old, "owner")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.SetAsyncAnswerer(func(context.Context, Inbound, string) (api.Receipt, error) {
		close(entered)
		<-release
		return api.Receipt{Outcome: "unknown"}, nil
	})
	var update api.Item
	s.SetAsyncUpdateObserver(func(item api.Item) { update = item })
	go func() {
		defer close(done)
		s.HandleDesktopQuestion(context.Background(), "original-unknown-request", "Q1", card.QuestionCards[0].Binding, "1", api.Snapshot{RuntimeOwner: "owner", Connection: "ready"})
	}()
	<-entered
	next := lifecycleQuestion()
	next.AsyncQuestions = []api.AsyncQuestion{{Title: "Updated question", Options: []string{"New one", "New two"}}}
	current, err := s.AsyncCard(next, "owner")
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if update.QuestionCards[0].Binding != current.QuestionCards[0].Binding {
		t.Fatal("late receipt overwrote new display", update)
	}
	s, err = Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.AsyncCard(old, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if replay.QuestionCards[0].Binding != current.QuestionCards[0].Binding || replay.QuestionCards[0].State != "" {
		t.Fatal("retired generation revived", replay)
	}
	prior := s.state.AsyncQuestions["Q1"]
	if prior.State != "unknown" || prior.ReplyID != "original-unknown-request" || !s.state.AsyncRetired[prior.Fingerprint] {
		t.Fatal("lost unknown receipt", prior)
	}
	if archived := s.state.AsyncClosed[asyncKey(prior)]; archived.State != "unknown" || archived.ReplyID != prior.ReplyID {
		t.Fatal("lost archived receipt", archived)
	}
}

func TestPersistedPresentationAliasesCannotHideOriginalConditions(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root, nil)
	original := lifecycleQuestion()
	original.AsyncQuestions[0] = api.AsyncQuestion{Title: "May I publish to the production website? Only after your review.", Options: []string{"Publish publicly to production", "Keep local"}}
	card, _ := s.AsyncWorkCard(original, "codex", "work")
	q := s.state.AsyncQuestions["Q1"]
	q.Presentation = &api.QuestionPresentation{Title: "Keep the demo local?", Options: []string{"Keep local", "Publish"}}
	s.state.AsyncQuestions["Q1"] = q
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	card.QuestionCards[0].Title = "Keep the demo local?"
	card.QuestionCards[0].Options = q.Presentation.Options
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	projected, _ := s.ProjectQuestions([]api.Item{card})
	got := projected[0].QuestionCards[0]
	if got.Title != q.Title || got.Options[0] != q.Options[0] || s.state.AsyncQuestions["Q1"].Presentation != nil {
		t.Fatal("persisted alias survived", got)
	}
	safe := &api.QuestionPresentation{Title: "May I publish to the production website?", Context: "Only after your review.", Options: q.Options}
	if _, err := s.PresentWorkerQuestion("Q1", safe); err != nil {
		t.Fatal("rejected lossless original conditions", err)
	}
	unsafe := *safe
	unsafe.Context = ""
	if _, err := s.PresentWorkerQuestion("Q1", &unsafe); err == nil {
		t.Fatal("hid original authorization condition")
	}
}
