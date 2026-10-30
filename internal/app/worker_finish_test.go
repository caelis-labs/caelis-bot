package app

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func TestProductFinishClosesQuestionAndPersistsAcrossRestart(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
	a, root := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	wireTestWorkers(t, a, pool)
	open := func() *tasks.Manager {
		m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		m.SetFinishedObserver(a.reconcileFinishedQuestions)
		a.tasks = m
		return m
	}
	m := open()
	work, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "finish-start", Title: "Redacted work", Prompt: "Original prompt unchanged", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	state := owner.states[work.ID]
	state.Runtime = "codex"
	state.ExecutionKey = "native-generation-one"
	state.Activity = "idle"
	state.Task.Status = "completed"
	state.AsyncQuestions = []api.Item{{ID: "question", TurnKey: "native-question-turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "Original question?", Options: []string{"Original option"}}}}}
	owner.states[work.ID] = state
	a.observeWorkerQuestions(t.Context())
	if len(a.WorkerQuestions()) != 1 || a.WorkerQuestions()[0].State != "" {
		t.Fatal("completed turn lost valid pending question")
	}
	finishedSnapshot := state
	finishedSnapshot.TaskEnded = true
	token := api.TaskFinishToken(state)
	if _, err := m.FinishTask(t.Context(), work.ID, "stale-token"); err == nil {
		t.Fatal("stale inspection finished task")
	}
	if _, err := m.FinishTask(t.Context(), work.ID, token); err != nil {
		t.Fatal(err)
	}
	if got := a.WorkerQuestions()[0]; got.State != "expired" || got.Reason != "task_completed" {
		t.Fatal("product finish did not close card", got)
	}
	m = open()
	a.observeWorkerQuestions(t.Context())
	if !a.workerStates()[0].TaskEnded || a.WorkerQuestions()[0].State != "expired" {
		t.Fatal("restart lost explicit finish")
	}
	// New native execution can produce a new question. The previous card stays closed.
	state.ExecutionKey = "native-generation-two"
	state.TurnKey = "new-turn"
	state.Task.Status = "working"
	state.Activity = "active"
	oldQuestion := state.AsyncQuestions[0]
	state.AsyncQuestions = append([]api.Item(nil), state.AsyncQuestions...)
	state.AsyncQuestions[0].ID = "new-question"
	state.AsyncQuestions[0].TurnKey = "new-turn"
	state.AsyncQuestions[0].AsyncCallID = "new-call"
	state.AsyncQuestions = append(state.AsyncQuestions, oldQuestion)
	owner.states[work.ID] = state
	a.observeWorkerQuestions(t.Context())
	a.observeWorkerQuestions(t.Context())
	// The original finish callback may arrive after terminal continuation admitted Q2.
	if err := a.reconcileFinishedQuestions(finishedSnapshot); err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, q := range a.WorkerQuestions() {
		if q.State == "" {
			pending++
		}
	}
	if pending != 1 || a.workerStates()[0].TaskEnded {
		t.Fatal("finish leaked into new execution")
	}
	if _, err := m.FinishTask(t.Context(), work.ID, token); err == nil {
		t.Fatal("old token finished later generation")
	}
	if len(e.reports) != 2 || !strings.Contains(e.reports[1].Text, "Q2") {
		t.Fatal("new question not notified exactly once", e.reports)
	}
	// Reopen both persisted controller and Manager. The original closed question
	// stays closed; the accepted private wakeup must not be resent.
	a.textControl, err = textchannel.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.textControl.SetAsyncWorkAnswerer(a.answerWorkerQuestion)
	a.Backend.SetQuestionLifecycle(a.textControl.ProjectQuestions, a.textControl.Revision, a.textControl.DismissQuestion)
	wireTestWorkers(t, a, pool)
	m = open()
	a.observeWorkerQuestions(t.Context())
	if len(e.reports) != 2 {
		t.Fatal("restart repeated question notice")
	}
	var current api.WorkerQuestion
	for _, q := range a.WorkerQuestions() {
		if q.State == "" {
			current = q
		} else if q.ID == "Q1" && q.State != "expired" {
			t.Fatal("closed original question revived")
		}
	}
	if current.ID != "Q2" {
		t.Fatal("current question missing after restart", current)
	}
	if receipt, err := a.PresentWorkerQuestion(t.Context(), current.ID, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	found := false
	for _, item := range a.Backend.ChatSnapshot(0, "", "").Snapshot.Items {
		for _, card := range item.QuestionCards {
			if card.ShortID == current.ID && card.State == "" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("new question did not reach Bot chat card")
	}
	if _, err := a.AnswerWorkerQuestion(t.Context(), current.ID+"@"+current.Binding, "1", "original-answer-operation"); err != nil {
		t.Fatal(err)
	}
	if owner.sends != 1 || !strings.Contains(owner.lastPrompt, `[\"request_user_input_async\",\"new-call\",0]`) {
		t.Fatal("answer lost original call binding", owner.lastPrompt)
	}
	if _, err := a.AnswerWorkerQuestion(t.Context(), current.ID+"@"+current.Binding, "1", "original-answer-operation"); err != nil {
		t.Fatal(err)
	}
	if owner.sends != 1 {
		t.Fatal("same answer operation replayed")
	}
}

// Exercise the production-shaped old ledger and real Manager finish boundary,
// rather than manually constructing TaskEnded=true in a controller unit test.
func TestRedactedQ23Q24MigrationThroughProductFinish(t *testing.T) {
	raw, err := os.ReadFile("../textchannel/testdata/v0183-control-redacted.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Control    map[string]json.RawMessage
		Tasks      []struct{ Question, Runtime, ID, Status, Execution, ReportState, QuestionTurn string }
		Provenance struct{ CapturedUTC string }
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	captured, _ := time.Parse(time.RFC3339Nano, f.Provenance.CapturedUTC)
	shift := time.Now().Unix() - captured.Unix()
	type source struct {
		ItemID, TurnKey, CallID, Owner, WorkID, Fingerprint, Title, State, ReplyID string
		Options                                                                    []string
		Index                                                                      int
		IssuedAt                                                                   int64 `json:"issuedAt"`
		ExpiresAt, ClosedAt                                                        int64
	}
	var sources map[string]source
	_ = json.Unmarshal(f.Control["asyncQuestions"], &sources)
	for _, key := range []string{"asyncQuestions", "asyncClosed"} {
		var records map[string]map[string]any
		_ = json.Unmarshal(f.Control[key], &records)
		for _, q := range records {
			for _, stamp := range []string{"issuedAt", "ExpiresAt", "ClosedAt"} {
				if v, ok := q[stamp].(float64); ok && v > 0 {
					q[stamp] = int64(v) + shift
				}
			}
		}
		f.Control[key], _ = json.Marshal(records)
	}
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	controlBytes, _ := json.Marshal(f.Control)
	if err := os.WriteFile(filepath.Join(root, "text-channel-control.json"), controlBytes, 0600); err != nil {
		t.Fatal(err)
	}
	a.textControl, err = textchannel.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	for _, task := range f.Tasks {
		q := sources[task.Question]
		owner.states[task.ID] = api.WorkState{Runtime: task.Runtime, ExecutionKey: task.Execution, Activity: "idle", PreviousReportState: task.ReportState, Task: api.Task{ID: task.ID, Title: "Redacted task", Status: task.Status, Outcome: "accepted", Result: "Redacted final result"}, AsyncQuestions: []api.Item{{ID: q.ItemID, TurnKey: q.TurnKey, Kind: "assistant", AsyncCallID: q.CallID, AsyncExpiresAt: q.ExpiresAt + shift, AsyncQuestions: []api.AsyncQuestion{{Title: q.Title, Options: q.Options}}}}}
	}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	wireTestWorkers(t, a, pool)
	m, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.tasks = m
	a.configureInteractionLifecycle(m)
	_ = m.ListTasks()
	a.observeWorkerQuestions(t.Context())
	for _, id := range []string{"Q23", "Q24"} {
		found := false
		for _, q := range a.WorkerQuestions() {
			if q.ID == id && q.State == "" {
				found = true
			}
		}
		if !found {
			t.Fatal("completed old task was prematurely guessed finished", id)
		}
	}
	for _, task := range f.Tasks {
		if task.Question != "Q23" && task.Question != "Q24" {
			continue
		}
		state := owner.states[task.ID]
		if _, err := m.FinishTask(t.Context(), task.ID, api.TaskFinishToken(state)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range a.WorkerQuestions() {
		if q.State == "" {
			t.Fatal("explicitly finished migrated question remained actionable", q.ID)
		}
	}
	a.textControl, err = textchannel.Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.observeWorkerQuestions(t.Context())
	for _, q := range a.WorkerQuestions() {
		if q.State == "" {
			t.Fatal("restart revived migrated question")
		}
	}
}
