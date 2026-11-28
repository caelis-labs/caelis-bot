package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type noWorkerShell struct{ t *testing.T }

func (s noWorkerShell) ShellObservationAvailable(context.Context) bool {
	s.t.Error("Worker ingress probed Shell")
	return false
}

func (s noWorkerShell) ObserveShell(context.Context, string) (json.RawMessage, error) {
	s.t.Error("Worker ingress invoked Shell")
	return nil, nil
}

func TestWorkerWakeRoutesUseOriginalReceiptsWithoutShell(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
	a, root := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	a.companion.ConfigureShellObserver(noWorkerShell{t})
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	wireTestWorkers(t, a, pool)
	manager, err := tasks.Open(filepath.Join(root, "task-notices.json"), filepath.Join(root, "Tasks"), "codex", pool, a.companion, a.Backend.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.tasks = manager
	work, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "wake-route-original", Title: "Original Worker", Prompt: "Observe a fixture", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	state := owner.states[work.ID]
	state.AsyncQuestions = []api.Item{{
		ID: "worker-question:original", TurnKey: state.ExecutionKey, Kind: "assistant", Status: "completed", AsyncCallID: "original-call",
		AsyncQuestions: []api.AsyncQuestion{{Title: "Which version?", Options: []string{"First", "Second"}}},
	}}
	owner.states[work.ID] = state
	e.snapshot.Approvals = []api.Approval{{
		ID: "native-approval", NativeID: "original-approval", TaskID: work.ID, Owner: "task", Status: "pending",
		Choices: []api.Choice{{ID: "allow", Scope: "once"}},
	}}
	for range 3 {
		a.observeWorkerQuestions(t.Context())
	}
	questions := a.WorkerQuestions()
	if len(questions) != 1 || len(e.reports) != 2 ||
		e.reports[0].ID != api.WorkerInteractionMessageID("approval", "native-approval") ||
		e.reports[0].SystemSource != api.SystemWorkerApproval || e.reports[1].SystemSource != api.SystemWorkerQuestion ||
		!strings.Contains(e.reports[1].Text, questions[0].ID) {
		t.Fatal("original approval/question notice was lost or repeated", e.reports)
	}
	if receipt, err := a.PresentWorkerQuestion(t.Context(), questions[0].ID, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	state.AsyncQuestions = nil
	state.Task.Status = "completed"
	state.Activity = "idle"
	owner.states[work.ID] = state
	e.snapshot.Approvals = nil
	for range 3 {
		if err := manager.DeliverTaskReport(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.reports) != 3 || e.reports[2].SystemSource != api.SystemWorkerResult || !strings.Contains(e.reports[2].Text, work.ID) {
		t.Fatal("Worker completion was misrouted or repeated", e.reports)
	}
	t.Log("3 interaction passes and 3 completion passes, 0 Shell calls, one wake of each original kind")
}
