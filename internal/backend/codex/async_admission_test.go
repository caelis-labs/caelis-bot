package codex

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoricalTerminalCannotMintQuestionAdmission(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	task := &taskRecord{Thread: "original-thread", Run: "old-turn", View: api.Task{ID: "task", Status: "completed"}}
	turn := nativeTurn{ID: "old-turn", Status: "completed", Items: []nativeItem{workerQuestionFixture()}}
	s.observeTaskTurn(task, turn)
	if task.AsyncAdmission.Turn != "" || len(task.AsyncQuestions) != 0 {
		t.Fatal("terminal replay minted a new lifetime")
	}
	s.observeTaskTurn(task, nativeTurn{ID: "fresh-turn", Status: "inProgress"})
	if task.AsyncAdmission.Turn != "fresh-turn" {
		t.Fatal("new live generation was not admitted")
	}
	deadline := task.AsyncAdmission.Deadline
	task.AsyncAdmission.Deadline = time.Now().Add(-time.Hour).Unix()
	s.observeTaskTurn(task, nativeTurn{ID: "fresh-turn", Status: "inProgress", Items: []nativeItem{workerQuestionFixture()}})
	if len(task.AsyncQuestions) != 0 || task.AsyncAdmission.Deadline == deadline {
		t.Fatal("same turn renewed its expired epoch")
	}
	s.observeTaskTurn(task, turn, true)
	if task.AsyncAdmission.Turn != "fresh-turn" || len(task.AsyncQuestions) != 0 {
		t.Fatal("historical read renewed an old epoch")
	}
}

func TestEpochOnlyChangeIsDurableAndLegacySourcesCannotAcquireLifetime(t *testing.T) {
	opts := SessionOptions{Directory: t.TempDir(), StateFile: filepath.Join(t.TempDir(), "binding.json")}
	s := NewSession(opts)
	task := &taskRecord{Thread: "original", Run: "current", Activity: "active", View: api.Task{ID: "work", Status: "working"}, Requests: map[string]taskReceipt{}}
	s.binding.Tasks = map[string]*taskRecord{"work": task}
	if !s.observeTaskTurn(task, nativeTurn{ID: "current", Status: "inProgress"}) {
		t.Fatal("epoch-only change was not marked dirty")
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	deadline := task.AsyncAdmission.Deadline
	s = NewSession(opts)
	task = s.binding.Tasks["work"]
	s.observeTaskTurn(task, nativeTurn{ID: "current", Status: "inProgress"})
	if task.AsyncAdmission.Deadline != deadline {
		t.Fatal("restart renewed original epoch")
	}
	task.AsyncQuestions = map[string]workerAsyncQuestion{"legacy": {Run: "legacy-turn", CallID: "legacy-call", ObservedAt: time.Now().Unix(), Questions: []api.AsyncQuestion{{Title: "legacy original"}}}}
	questions := s.WorkStates()[0].AsyncQuestions
	if len(questions) != 1 || questions[0].AsyncExpiresAt >= 0 {
		t.Fatal("legacy cache was granted a new source lifetime")
	}
}
