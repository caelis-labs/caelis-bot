package codex

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

// Exercise the real adapter projection, durable question controller, field reply,
// later native commentary/final turn and restart. No model or channel is used.
func TestWorkerAnswerContinuationDoesNotEndUnansweredQuestions(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: root, StateFile: filepath.Join(root, "binding.json")}
	s := NewSession(opts)
	task := &taskRecord{Thread: "original-worker", Run: "question-turn", View: api.Task{ID: "work", Status: "working"}}
	s.binding.Tasks = map[string]*taskRecord{"work": task}
	s.observeTaskTurn(task, nativeTurn{ID: "question-turn", Status: "completed", Items: []nativeItem{{ID: "call", Type: "agentMessage", Delivery: "async", Questions: []struct {
		Title   string   `json:"title"`
		Options []string `json:"options"`
	}{{Title: "Which format?", Options: []string{"Video", "Images"}}, {Title: "Which connection?", Options: []string{"Manual login", "Existing connection"}}}}}})
	store, err := textchannel.Open(filepath.Join(root, "control"), nil)
	if err != nil {
		t.Fatal(err)
	}
	project := func() []api.WorkState {
		v := s.WorkStates()
		for i := range v {
			v[i].Runtime = "codex"
		}
		return v
	}
	work := project()
	card, err := store.AsyncWorkCard(work[0].AsyncQuestions[0], "codex", "work")
	if err != nil {
		t.Fatal(err)
	}
	store.SetAsyncWorkAnswerer(func(context.Context, textchannel.Inbound, string, string, string, string) (api.Receipt, error) {
		return api.Receipt{Outcome: "accepted"}, nil
	})
	store.HandleDesktopQuestion(t.Context(), "first-answer", card.QuestionCards[0].ShortID, card.QuestionCards[0].Binding, "1", api.Snapshot{})
	for _, phase := range []string{"commentary", "final_answer"} {
		s.observeTaskTurn(task, nativeTurn{ID: "continuation-" + phase, Status: "completed", Items: []nativeItem{{ID: "reply", Type: "agentMessage", Phase: phase, Text: "The first choice is recorded; I still need your connection answer."}}})
		if err := s.save(); err != nil {
			t.Fatal(err)
		}
		s = NewSession(opts)
		task = s.binding.Tasks["work"]
		if err := store.ReconcileAsync(api.Snapshot{}, project()); err != nil {
			t.Fatal(err)
		}
		store, err = textchannel.Open(filepath.Join(root, "control"), nil)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := store.ProjectQuestions([]api.Item{card})
		if got[0].QuestionCards[0].State != "answered" || got[0].QuestionCards[1].State != "" {
			t.Fatalf("%s ended sibling: %+v", phase, got)
		}
	}
	task.Retired = true
	if err := store.ReconcileAsync(api.Snapshot{}, project()); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ProjectQuestions([]api.Item{card})
	if got[0].QuestionCards[1].State != "expired" {
		t.Fatal("explicit retirement did not close question", got)
	}
}
