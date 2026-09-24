package bot

import (
	"errors"
	"testing"
	"time"
)

func TestUpdatePauseFencesDueReminderAndResumePreservesIt(t *testing.T) {
	r, engine, now := fixture(t)
	saveReminder(t, r, "due")
	*now = now.Add(time.Minute)
	if err := r.PauseIfIdle(func() error { return errors.New("busy") }); err == nil {
		t.Fatal("guard ignored")
	}
	if err := r.PauseIfIdle(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(engine.submissions) != 0 || r.State().Wake != nil {
		t.Fatal("reminder crossed update fence")
	}
	r.ResumeAfterUpdate()
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(engine.submissions) != 1 {
		t.Fatal("reminder lost on cancelled update")
	}
}
