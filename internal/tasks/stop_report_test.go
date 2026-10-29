package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func TestOnlyAcknowledgedOriginalInterruptionSuppressesReport(t *testing.T) {
	for _, test := range []struct {
		name, status string
		ack, pending bool
		want         int
	}{
		{"stopped", "interrupted", true, false, 0},
		{"unknown_stop", "interrupted", false, false, 1},
		{"pending_stop", "interrupted", true, true, 1},
		{"completion_race", "completed", true, false, 1},
		{"failure_race", "failed", true, false, 1},
		{"spontaneous_interruption", "interrupted", false, false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, f := t.TempDir(), newRuntime()
			m := openFixture(t, root, "codex", f)
			v := start(t, m, "stop-notice-task")
			current := f.states[v.ID]
			current.Task.Status = test.status
			current.StopRequested = true
			current.StopAcknowledged = test.ack
			current.StopPending = test.pending
			current.StopResolution = test.status
			f.states[v.ID] = current
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			m = openFixture(t, root, "codex", f)
			for range 2 {
				if err := m.DeliverTaskReport(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if f.reports != test.want {
				t.Fatal("wrong report count", f.reports)
			}
			current.ExecutionKey = "later-turn"
			current.Task.Status = "interrupted"
			current.StopAcknowledged = false
			current.StopPending = false
			current.StopResolution = ""
			f.states[v.ID] = current
			if err := m.DeliverTaskReport(t.Context()); err != nil {
				t.Fatal(err)
			}
			if f.reports != test.want+1 {
				t.Fatal("later native turn report suppressed")
			}
		})
	}
}

func TestFailureReportUsesPersistedSafeProjection(t *testing.T) {
	root, f := t.TempDir(), newRuntime()
	m := openFixture(t, root, "codex", f)
	v := start(t, m, "failure-report-task")
	current := f.states[v.ID]
	current.Task.Status = "failed"
	current.Task.Failure = api.UnknownTaskFailure("caelis", "original-turn")
	f.states[v.ID] = current
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	m = openFixture(t, root, "codex", f)
	if m.state.Records[v.ID].View.Failure.Execution != "original-turn" {
		t.Fatal("task ledger lost failure")
	}
}

func TestLocalCreationRefusalBeforeDispatchKeepsUnknownReceipts(t *testing.T) {
	f := newRuntime()
	root := t.TempDir()
	m := openFixture(t, root, "codex", f)
	_, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "missing-directory", Title: "Create", Prompt: "Create", Workspace: filepath.Join(root, "absent")})
	var refusal *api.WorkMessageRejection
	if !errors.As(err, &refusal) || f.starts != 0 {
		t.Fatal("local refusal reported uncertain", err)
	}
	id := "task-" + hash("codex", "occupied-directory")
	if err := os.MkdirAll(filepath.Join(root, "Tasks", id), 0700); err != nil {
		t.Fatal(err)
	}
	v, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "occupied-directory", Title: "Create", Prompt: "Create"})
	if !errors.As(err, &refusal) || v.Outcome != "rejected" || f.starts != 0 {
		t.Fatal("allocated workspace refusal reported uncertain", v, err)
	}
	f.unknownStart = true
	v, err = m.StartTask(t.Context(), api.TaskStart{RequestID: "unknown-original", Title: "Create", Prompt: "Create"})
	if err == nil || v.Outcome != "unknown" {
		t.Fatal(v, err)
	}
	again, err := m.StartTask(t.Context(), api.TaskStart{RequestID: "unknown-original", Title: "Create", Prompt: "Create"})
	if err != nil || again.Outcome != "unknown" || f.starts != 1 {
		t.Fatal("unknown creation replayed", again, err)
	}
}

type reportRefusalRuntime struct {
	*fixtureRuntime
	reject bool
}

func (f *reportRefusalRuntime) SubmitSystem(_ context.Context, in api.SystemMessage) (api.Receipt, error) {
	f.reports++
	f.lastReport = in.NativeInput()
	outcome := "accepted"
	if f.reject {
		outcome = "rejected"
	}
	return api.Receipt{ID: in.ID, Outcome: outcome}, nil
}
func TestFailureReportFreezesOriginalTextBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	f := &reportRefusalRuntime{fixtureRuntime: newRuntime(), reject: true}
	open := func() *Manager {
		m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := open()
	v := start(t, m, "stable-report-task")
	state := f.states[v.ID]
	state.Task.Status = "failed"
	f.states[v.ID] = state
	if err := m.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	original := f.lastReport
	state.Task.Failure = api.UnknownTaskFailure("codex", "native-turn-1")
	f.states[v.ID] = state
	m = open()
	f.reject = false
	if err := m.DeliverTaskReport(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.lastReport.ID != original.ID || f.lastReport.Text != original.Text {
		t.Fatal("same report identity changed bytes")
	}
	if err := m.DeliverTaskReport(t.Context()); err != nil || f.reports != 2 {
		t.Fatal("report replayed", err, f.reports)
	}
}

func TestCreationAdmissionRefusalKeepsOriginalUnknown(t *testing.T) {
	for _, kind := range []string{"capacity", "paused", "native_admission", "source"} {
		t.Run(kind, func(t *testing.T) {
			f := newRuntime()
			m := openFixture(t, t.TempDir(), "codex", f)
			log := diagnosticlog.New(t.TempDir())
			m.ConfigureDiagnostics(log)
			f.unknownStart = true
			original, err := m.StartTask(t.Context(), input("original-unknown"))
			if err == nil || original.Outcome != "unknown" {
				t.Fatal(original, err)
			}
			f.unknownStart = false
			switch kind {
			case "capacity":
				for i := 1; i < 6; i++ {
					start(t, m, fmt.Sprintf("existing-request-%d", i))
				}
			case "paused":
				m.paused = true
			case "source":
				f.admit = &api.WorkAdmissionError{Code: "source_not_authorized", Cause: errors.New("summary cannot authorize work")}
			case "native_admission":
				f.admit = errors.New("native admission unavailable")
			}
			before := f.starts
			denied, err := m.StartTask(t.Context(), input("new-admission"))
			var refusal *api.WorkMessageRejection
			wantReason := map[string]string{"capacity": "capacity_full", "paused": "updater_paused", "native_admission": "work_admission_failed", "source": "source_not_authorized"}[kind]
			if !errors.As(err, &refusal) || refusal.Code != wantReason || refusal.Cause == nil || denied.Outcome != "rejected" || f.starts != before {
				t.Fatal("pre-dispatch refusal became uncertain", denied, err)
			}
			same, err := m.StartTask(t.Context(), input("original-unknown"))
			if errors.As(err, &refusal) || same.ID != original.ID || same.Outcome != "unknown" || f.starts != before {
				t.Fatal("admission refusal erased original uncertainty", same, err)
			}
			if log.Status()["written"] != uint64(1) {
				t.Fatal("original unknown was logged as a fresh refusal", log.Status())
			}
		})
	}
}
