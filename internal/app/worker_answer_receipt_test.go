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
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

type answerReceiptOwner struct {
	*workerFixture
	err        error
	requestIDs []string
}

func (w *answerReceiptOwner) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	w.requestIDs = append(w.requestIDs, in.RequestID)
	return w.states[in.ID].Task, w.err // Deliberately carries an old accepted task view.
}

func TestWorkerAnswerClassifiesCurrentRequestWithoutReplayingUnknown(t *testing.T) {
	for _, rejected := range []bool{true, false} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
			logs := t.TempDir()
			a, root := fixtureApp(t, e, Host{Diagnostics: diagnosticlog.New(logs)})
			base := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
			owner := &answerReceiptOwner{workerFixture: base, err: errors.New("fixture lost receipt")}
			if rejected {
				owner.err = &api.WorkMessageRejection{Cause: errors.New("fixture not dispatched")}
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
				in.ID = "competing-callback"
				_ = a.textControl.Handle(t.Context(), in, api.Snapshot{})
			}
			sum := sha256.Sum256([]byte("telegram\x00fixture\x00original-callback\x00" + work.ID + "\x00" + item.ID))
			if len(owner.requestIDs) != 1 || owner.requestIDs[0] != fmt.Sprintf("worker-answer-%x", sum[:]) {
				t.Fatal("original answer ID changed or prompt replayed")
			}
			for _, row := range a.Backend.ChatSnapshot(0, "").Snapshot.Items {
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
	found := map[string]string{}
	for _, row := range a.Backend.ChatSnapshot(0, "").Snapshot.Items {
		if row.Kind == "user" {
			found[row.RequestID] = row.Text
		}
	}
	if found[ordinary.ID] != ordinary.Text || found[missing.ID] != "回答内容已隐藏" {
		t.Fatal("ordinary/unknown answer display classification failed")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(root, "chat.sqlite"))
	if err != nil || strings.Contains(string(disk), "SYNTHETIC_PRIVATE_VALUE") {
		t.Fatal("unknown field value entered durable IM")
	}
}
