package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

// Real Session wire -> Manager finish -> persistent question Store. Only the
// native Runtime peer is controlled; no external model or account is used.
func TestFinishedTaskContinuationKeepsNewNativeQuestion(t *testing.T) {
	for _, via := range []string{"bot", "terminal", "legacy-bot", "legacy-terminal"} {
		t.Run(via, func(t *testing.T) {
			s, f, d, _ := taskPair(t)
			catalog := filepath.Join(t.TempDir(), "tasks.json")
			m, err := tasks.Open(catalog, s.workRoot(), "codex", s, s, s.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			store, err := textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			reconcile := func(state api.WorkState) error {
				state.Runtime = "codex"
				for _, item := range state.AsyncQuestions {
					if _, err := store.AsyncWorkCard(item, "codex", state.Task.ID); err != nil {
						return err
					}
				}
				return store.ReconcileAsync(api.Snapshot{}, []api.WorkState{state})
			}
			m.SetFinishedObserver(reconcile)
			sendSynthetic(t, s, "finish-parent")
			task := newTask(t, m, "finish-start")
			s.mu.Lock()
			thread, oldRun := s.binding.Tasks[task.ID].Thread, s.binding.Tasks[task.ID].Run
			s.mu.Unlock()
			f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": oldRun, "item": workerQuestionFixture()})})
			awaitState(t, s, func(api.Snapshot) bool { return len(s.WorkStates()[0].AsyncQuestions) == 1 })
			finishTask(s, f, task.ID, "completed")
			awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "completed" })
			state := s.WorkStates()[0]
			if _, err := m.FinishTask(t.Context(), task.ID, api.TaskFinishToken(state)); err != nil {
				t.Fatal(err)
			}
			if !m.InteractionStates(s.WorkStates())[0].TaskEnded {
				t.Fatal("original generation not finished")
			}
			if strings.HasPrefix(via, "legacy-") {
				// Production-shaped old catalog: the completion report was also persisted
				// as both execution and explicit finish. The native binding is unchanged.
				b, err := os.ReadFile(catalog)
				if err != nil {
					t.Fatal(err)
				}
				var disk map[string]any
				if err = json.Unmarshal(b, &disk); err != nil {
					t.Fatal(err)
				}
				record := disk["records"].(map[string]any)[task.ID].(map[string]any)
				record["execution"], record["finishedExecution"] = state.LegacyExecutionKey, state.LegacyExecutionKey
				b, err = json.Marshal(disk)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(catalog, b, 0600); err != nil {
					t.Fatal(err)
				}
				m, err = tasks.Open(catalog, s.workRoot(), "codex", s, s, s.Snapshot)
				if err != nil {
					t.Fatal(err)
				}
				m.SetFinishedObserver(reconcile)
			}
			nextRun := "terminal-next-run"
			if strings.HasSuffix(via, "bot") {
				if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: task.ID, RequestID: "finish-continue", Prompt: "Continue original task"}); err != nil {
					t.Fatal(err)
				}
				s.mu.Lock()
				nextRun = s.binding.Tasks[task.ID].Run
				s.mu.Unlock()
			} else {
				f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: nextRun, Status: "inProgress"}})})
			}
			nextQuestion := workerQuestionFixture()
			nextQuestion.ID = "new-original-call"
			f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": nextRun, "item": nextQuestion})})
			awaitState(t, s, func(api.Snapshot) bool { return len(s.WorkStates()[0].AsyncQuestions) == 2 })
			f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": thread, "turn": nativeTurn{ID: oldRun, Status: "completed"}})})
			current := s.WorkStates()[0]
			if current.Task.Status != "working" || current.ExecutionKey == state.ExecutionKey || current.TurnKey == state.TurnKey {
				t.Fatal("native generation did not advance", current)
			}
			states := m.InteractionStates(s.WorkStates())
			if states[0].TaskEnded {
				t.Fatal("old finish closed new execution")
			}
			if err = reconcile(states[0]); err != nil {
				t.Fatal(err)
			}
			assertQuestions := func() {
				t.Helper()
				pending, closed := 0, 0
				for _, q := range store.WorkerAsyncQuestions() {
					if q.State == "" {
						pending++
					} else if q.State == "expired" && q.Reason == "task_completed" {
						closed++
					}
				}
				if pending != 1 || closed != 1 {
					t.Fatalf("want old closed and new pending, got %+v", store.WorkerAsyncQuestions())
				}
			}
			assertQuestions()
			// Reopen the persisted adapter/catalog/controller, then deliver a late old
			// terminal event. It cannot restore the prior finish or renew question age.
			loaded := NewSession(s.opts)
			defer loaded.cancelLife()
			loaded.resetProjection()
			retained := loaded.binding.Tasks[task.ID]
			loaded.observeTaskTurn(retained, nativeTurn{ID: oldRun, Status: "completed"})
			if retained.Run != nextRun {
				t.Fatal("late old completion rewound persisted native turn")
			}
			reopened, err := tasks.Open(catalog, s.workRoot(), "codex", loaded, loaded, loaded.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if reopened.InteractionStates(loaded.WorkStates())[0].TaskEnded {
				t.Fatal("restart restored stale finish")
			}
			store, err = textchannel.Open(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = reconcile(reopened.InteractionStates(loaded.WorkStates())[0]); err != nil {
				t.Fatal(err)
			}
			assertQuestions()
			// The new question is answerable through the same task and native call, and
			// retrying its exact request ID observes the original receipt without replay.
			f.mu.Lock()
			before := d.sends
			f.mu.Unlock()
			in := api.TaskMessage{ID: task.ID, RequestID: "original-answer", Prompt: "Synthetic answer"}
			ctx := api.WithWorkerQuestionAnswer(t.Context(), "new-original-call")
			for range 2 {
				v, err := m.SendTask(ctx, in)
				if err != nil || v.Outcome != "accepted" {
					t.Fatal("new original answer rejected", v, err)
				}
			}
			f.mu.Lock()
			after := d.sends
			f.mu.Unlock()
			if after != before+1 {
				t.Fatal("original answer replayed", before, after)
			}
		})
	}
}
