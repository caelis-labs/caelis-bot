package tasks

import (
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

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
