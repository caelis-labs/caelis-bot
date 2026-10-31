package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/chatlog"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

type answerReceiptOwner struct {
	*workerFixture
	err        error
	outcome    string
	requestIDs []string
}

func (w *answerReceiptOwner) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	w.requestIDs = append(w.requestIDs, in.RequestID)
	v := w.states[in.ID].Task // Deliberately carries an old accepted task view by default.
	if w.outcome != "" {
		v.Outcome = w.outcome
	}
	return v, w.err
}

func TestWorkerAnswerClassifiesCurrentRequestWithoutReplayingUnknown(t *testing.T) {
	for _, outcome := range []string{"rejected", "conflicted", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			rejected := outcome != "unknown"
			e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
			logs := t.TempDir()
			a, root := fixtureApp(t, e, Host{Diagnostics: diagnosticlog.New(logs)})
			base := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
			owner := &answerReceiptOwner{workerFixture: base, err: errors.New("fixture lost receipt")}
			if rejected {
				owner.err = &api.WorkMessageRejection{Cause: errors.New("fixture not dispatched")}
			}
			if outcome == "conflicted" {
				owner.outcome = outcome
				owner.err = &api.WorkMessageRejection{Code: "native_conflict", Cause: errors.New("fixture conflict")}
			}
			pool := workPool(root, "codex", map[string]*workerFixture{"codex": base})
			pool.resident = owner
			manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			wireTestWorkers(t, a, pool)
			a.tasks = manager
			work, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "fixture-start", Title: "Synthetic", Prompt: "Synthetic question", Workspace: root})
			if err != nil {
				t.Fatal(err)
			}
			state := base.states[work.ID]
			state.Runtime = "codex"
			state.Activity = "idle"
			state.Task.Status = "completed"
			base.states[work.ID] = state
			a.configureInteractionLifecycle(manager)
			if _, err = manager.FinishTask(t.Context(), work.ID, api.TaskFinishToken(state)); err != nil {
				t.Fatal(err)
			}
			state.ExecutionKey = "new-question-execution"
			state.Activity = "active"
			state.Task.Status = "working"
			item := api.Item{ID: "original-question", TurnKey: "original-turn", Kind: "assistant", AsyncCallID: "original-call", AsyncQuestions: []api.AsyncQuestion{{Title: "Synthetic?", Options: []string{"One", "Two"}}}}
			state.AsyncQuestions = []api.Item{item}
			base.states[work.ID] = state
			a.observeWorkerQuestions(t.Context())
			in := textchannel.Inbound{Channel: "telegram", Conversation: "fixture", ID: "original-callback", Text: "/answer Q1 1"}
			feedback := a.textControl.Handle(t.Context(), in, api.Snapshot{})
			diagnostic, logErr := os.ReadFile(filepath.Join(logs, "error.jsonl"))
			if logErr != nil || !strings.Contains(string(diagnostic), "worker_question") || strings.Contains(string(diagnostic), owner.err.Error()) {
				t.Fatal("answer failure lacks privacy-safe diagnostic", logErr)
			}
			q := a.textControl.AsyncButtonQuestions(item.ID)[0]
			if rejected && (q.State != "" || !strings.Contains(feedback, "未提交")) || !rejected && (q.State != "unknown" || !strings.Contains(feedback, "待核对")) {
				t.Fatal("incorrect current-answer outcome")
			}
			_ = a.textControl.Handle(t.Context(), in, api.Snapshot{})
			if !rejected {
				// A restart keeps the exact unknown answer receipt, not the old finish.
				a.textControl, err = textchannel.Open(root, nil)
				if err != nil {
					t.Fatal(err)
				}
				a.textControl.SetAsyncWorkAnswerer(a.answerWorkerQuestion)
				in.ID = "competing-callback"
				_ = a.textControl.Handle(t.Context(), in, api.Snapshot{})
			}
			sum := sha256.Sum256([]byte("telegram\x00fixture\x00original-callback\x00" + work.ID + "\x00" + item.ID))
			if len(owner.requestIDs) != 1 || owner.requestIDs[0] != fmt.Sprintf("worker-answer-%x", sum[:]) {
				t.Fatal("original answer ID changed or prompt replayed")
			}
			for _, row := range a.Backend.ChatSnapshot(0, "", "").Snapshot.Items {
				if row.Kind == "user" && strings.Contains(row.Text, "敏感字段") {
					t.Fatal("ordinary async answer mislabeled secret")
				}
			}
		})
	}
}

func TestAsyncInputAndUnknownFieldHaveAccurateDurableDisplay(t *testing.T) {
	a, root := fixtureApp(t, newTestEngine(), Host{})
	_, err := a.textControl.AsyncCard(api.Item{ID: "ordinary-question", TurnKey: "turn", Kind: "assistant", AsyncCallID: "call", AsyncQuestions: []api.AsyncQuestion{{Title: "Synthetic?"}}}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	ordinary := textchannel.Inbound{Channel: "telegram", Conversation: "fixture", ID: "ordinary-answer", Text: "/answer Q1 Synthetic ordinary answer"}
	missing := textchannel.Inbound{Channel: "telegram", Conversation: "fixture", ID: "missing-field", Text: "/answer Q999 SYNTHETIC_PRIVATE_VALUE"}
	_ = a.textControl.Handle(t.Context(), ordinary, api.Snapshot{})
	_ = a.textControl.Handle(t.Context(), missing, api.Snapshot{})
	assertDisplay := func(items []api.Item) {
		t.Helper()
		found := map[string]string{}
		for _, row := range items {
			if row.Kind == "user" {
				found[row.RequestID] = row.Text
			}
		}
		if found[ordinary.ID] != ordinary.Text || found[missing.ID] != "回答内容已隐藏" {
			t.Fatal("ordinary/unknown answer display classification failed")
		}
	}
	assertDisplay(waitChatInputs(t, func() []api.Item { return a.Backend.ChatSnapshot(0, "", "").Snapshot.Items }, ordinary.ID, missing.ID))
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(root, "chat.sqlite"))
	if err != nil || strings.Contains(string(disk), "SYNTHETIC_PRIVATE_VALUE") {
		t.Fatal("unknown field value entered durable IM")
	}
	reopened := chatlog.Open(filepath.Join(root, "chat.sqlite"))
	defer reopened.Close()
	assertDisplay(waitChatInputs(t, func() []api.Item { items, _ := reopened.Snapshot(); return items }, ordinary.ID, missing.ID))
}

type resolvingAnswerOwner struct {
	*answerReceiptOwner
	reads int
}

func (w *resolvingAnswerOwner) ReadWorkOperation(_ context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	w.reads++
	return api.WorkOperation{WorkOperationQuery: q, Outcome: "accepted"}, nil
}

func TestOriginalOwnerProofSettlesAnswersAndReleasesAttemptBudget(t *testing.T) {
	e := newTestEngine()
	a, root := fixtureApp(t, e, Host{})
	base := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	owner := &resolvingAnswerOwner{answerReceiptOwner: &answerReceiptOwner{workerFixture: base, err: errors.New("synthetic lost answer receipt")}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": base})
	pool.resident = owner
	wireTestWorkers(t, a, pool)
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	a.tasks = manager
	a.configureInteractionLifecycle(manager)
	work, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "reconcile-start", Title: "Synthetic", Prompt: "Original", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 12; n++ {
		item := api.Item{ID: fmt.Sprint("receipt-source-", n), TurnKey: "original-turn", Kind: "assistant", AsyncCallID: fmt.Sprint("receipt-call-", n), AsyncQuestions: []api.AsyncQuestion{{Title: "Synthetic?", Options: []string{"One", "Two"}}}}
		state := base.states[work.ID]
		state.AsyncQuestions = []api.Item{item}
		base.states[work.ID] = state
		a.observeWorkerQuestions(t.Context())
		var q api.WorkerQuestion
		for _, candidate := range a.WorkerQuestions() {
			if candidate.State == "" {
				q = candidate
				break
			}
		}
		if q.ID == "" {
			t.Fatal("source not actionable")
		}
		_, err := a.AnswerWorkerQuestion(t.Context(), q.ID+"@"+q.Binding, "2", fmt.Sprint("original-operation-", n))
		if err != nil {
			t.Fatal(err)
		}
		if len(owner.requestIDs) != n+1 {
			t.Fatal("answer was duplicated")
		}
		a.observeWorkerQuestions(t.Context())
		a.observeWorkerQuestions(t.Context())
		if len(a.textControl.AsyncAnswerReceipts()) != 0 || len(a.questionReceiptAttempts) != 0 {
			t.Fatal("settled answer retained receipt/attempt entry")
		}
	}
	if owner.reads != 12 || len(owner.requestIDs) != 12 {
		t.Fatal("reconciliation replayed native input or omitted original receipt", owner.reads)
	}
}
