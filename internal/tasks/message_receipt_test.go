package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type messageReceiptRuntime struct {
	*fixtureRuntime
	result    api.Task
	err       error
	calls     int
	requestID string
	recorded  bool
}

func (f *messageReceiptRuntime) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.calls++
	f.requestID = in.RequestID
	return f.result, f.err
}
func (f *messageReceiptRuntime) WorkMessageRecorded(api.TaskMessage) bool { return f.recorded }

func TestMessageOutcomeNeverInheritsPreviousTaskAcceptance(t *testing.T) {
	for _, kind := range []string{"predispatch", "lost-receipt", "old-accepted-error", "wrong-target", "accepted", "native-rejected", "native-conflicted", "typed-conflicted"} {
		t.Run(kind, func(t *testing.T) {
			root, base := t.TempDir(), newRuntime()
			f := &messageReceiptRuntime{fixtureRuntime: base}
			open := func() *Manager {
				m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			m := open()
			work := start(t, m, "initial-request")
			want := "unknown"
			switch kind {
			case "predispatch":
				f.err = &api.WorkMessageRejection{Cause: errors.New("fixture did not dispatch")}
				want = "rejected"
			case "lost-receipt":
				f.err = errors.New("fixture lost reply")
			case "old-accepted-error":
				f.result, f.err = work, errors.New("fixture transport error")
			case "wrong-target":
				f.result = api.Task{ID: "another-task", Outcome: "accepted"}
				f.err = &api.WorkMessageRejection{Cause: errors.New("unrelated rejection")}
			case "accepted", "native-rejected", "native-conflicted", "typed-conflicted":
				want = "accepted"
				if kind == "native-rejected" {
					want = "rejected"
				}
				if kind == "native-conflicted" || kind == "typed-conflicted" {
					want = "conflicted"
				}
				if kind == "typed-conflicted" {
					f.err = &api.WorkMessageRejection{Code: "native_conflict", Cause: errors.New("native intent conflict")}
				}
				f.result = api.Task{ID: work.ID, Status: "working", Outcome: want}
			}
			in := api.TaskMessage{ID: work.ID, RequestID: "original-answer-request", Prompt: "Synthetic answer"}
			got, _ := m.SendTask(t.Context(), in)
			if got.Outcome != want || f.requestID != in.RequestID || f.calls != 1 {
				t.Fatal("message outcome used old task state or changed identity")
			}
			// Restart plus the original request must never dispatch again when
			// its owner has no recorded receipt, including the unknown case.
			m = open()
			got, _ = m.SendTask(t.Context(), in)
			if got.Outcome != want || f.calls != 1 {
				t.Fatal("original request was replayed after restart")
			}
		})
	}
}

type conflictReceiptRuntime struct{ *messageReceiptRuntime }

func (f *conflictReceiptRuntime) ReadWorkOperation(_ context.Context, q api.WorkOperationQuery) (api.WorkOperation, error) {
	return api.WorkOperation{WorkOperationQuery: q, Outcome: "conflicted"}, nil
}

func TestConflictedReconciliationSettlesWithoutReplay(t *testing.T) {
	for _, explicitRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "send", true: "read"}[explicitRead], func(t *testing.T) {
			root := t.TempDir()
			f := &conflictReceiptRuntime{&messageReceiptRuntime{fixtureRuntime: newRuntime(), err: errors.New("lost receipt")}}
			open := func() *Manager {
				m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			m := open()
			work := start(t, m, "original-start")
			in := api.TaskMessage{ID: work.ID, RequestID: "original-send", Prompt: "Synthetic answer"}
			if got, _ := m.SendTask(t.Context(), in); got.Outcome != "unknown" {
				t.Fatal(got)
			}
			if explicitRead {
				observed := ""
				m.SetOperationObserver(func(v api.WorkOperation) error { observed = v.Outcome; return nil })
				got, err := m.ReconcileTaskRequest(t.Context(), in.ID, in.RequestID)
				if err != nil || got.Outcome != "conflicted" || observed != "conflicted" {
					t.Fatal(got, err, observed)
				}
			} else {
				got, err := m.SendTask(t.Context(), in)
				if err != nil || got.Outcome != "conflicted" {
					t.Fatal(got, err)
				}
			}
			m = open()
			if got, err := m.SendTask(t.Context(), in); err != nil || got.Outcome != "conflicted" || f.calls != 1 {
				t.Fatal("conflict was collapsed or replayed", got, err, f.calls)
			}
		})
	}
}

func TestRefusedReconciliationCannotRejectEarlierUnknownDispatch(t *testing.T) {
	base := newRuntime()
	f := &messageReceiptRuntime{fixtureRuntime: base, err: errors.New("fixture lost reply")}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	work := start(t, m, "initial-request")
	in := api.TaskMessage{ID: work.ID, RequestID: "original-unknown", Prompt: "Synthetic answer"}
	_, _ = m.SendTask(t.Context(), in)
	f.recorded = true
	f.err = &api.WorkMessageRejection{Cause: errors.New("reconciliation unavailable before owner lookup")}
	got, _ := m.SendTask(t.Context(), in)
	if got.Outcome != "unknown" {
		t.Fatal("failed lookup replaced original unknown with rejected")
	}
	m.paused = true
	got, _ = m.SendTask(t.Context(), in)
	if got.Outcome != "unknown" || f.calls != 2 {
		t.Fatal("paused lookup erased earlier dispatch evidence")
	}
}
