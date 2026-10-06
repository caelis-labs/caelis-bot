package tasks

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPauseForUpdatePreservesActiveReceiptAndRecoversAdmission(t *testing.T) {
	f := newRuntime()
	root := t.TempDir()
	m := openFixture(t, root, "fixture", f)
	v := start(t, m, "update-active-request")
	guarded := false
	if err := m.PauseForUpdate(func() error {
		guarded = true
		b, err := os.ReadFile(filepath.Join(root, "tasks.json"))
		if err != nil || !strings.Contains(string(b), v.ID) || !strings.Contains(string(b), "update-active-request") {
			t.Fatal("original owner and request were not saved before guard", err)
		}
		return nil
	}); err != nil || !guarded {
		t.Fatal(err, guarded)
	}
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "during-update", Prompt: "continue"}); err == nil {
		t.Fatal("continuation crossed update fence")
	}
	if f.states[v.ID].Task.Status != "working" {
		t.Fatal("active native work was stopped")
	}
	m.ResumeAfterUpdate()
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "after-cancel", Prompt: "continue"}); err != nil {
		t.Fatal("admission was not restored", err)
	}
}

func TestPauseForUpdatePersistenceFailureDoesNotFreeze(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "fixture", f)
	m.write = func() error { return errors.New("disk full") }
	called := false
	if err := m.PauseForUpdate(func() error { called = true; return nil }); err == nil || !strings.Contains(err.Error(), "disk full") || called {
		t.Fatal("failed persistence admitted update", err, called)
	}
	m.write = m.save
	start(t, m, "after-failed-update")
}

func TestUpdateFencesTaskStartContinuationAndReports(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "fixture", f)
	v := start(t, m, "first-task")
	if err := m.PauseIfIdle(func() error { return nil }); err == nil {
		t.Fatal("admitted active task")
	}
	f.complete(v.ID)
	if err := m.PauseIfIdle(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartTask(t.Context(), input("second-task")); err == nil {
		t.Fatal("new work crossed fence")
	}
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "followup", Prompt: "continue"}); err == nil {
		t.Fatal("continuation crossed fence")
	}
	if err := m.DeliverTaskReport(t.Context()); err != nil || f.reports != 0 {
		t.Fatal("report crossed fence", err)
	}
	if f.starts != 1 || f.states[v.ID].Task.Status != "completed" {
		t.Fatal("native work changed")
	}
	m.ResumeAfterUpdate()
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "followup", Prompt: "continue"}); err != nil {
		t.Fatal(err)
	}
	if f.states[v.ID].Task.Status != "working" {
		t.Fatal("cancel did not resume admission")
	}
}

func TestUpdateCannotUseFailedProjectionAsIdleAuthority(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "fixture", f)
	writes := m.write
	m.write = func() error { return errors.New("disk full") }
	if err := m.PauseIfIdle(func() error { return nil }); err == nil {
		t.Fatal("persistence error ignored")
	}
	m.write = writes
	if err := m.PauseIfIdle(func() error { return errors.New("resident busy") }); err == nil {
		t.Fatal("guard ignored")
	}
	start(t, m, "still-open")
}
