package desktop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
)

type taskFakeDriver struct {
	*fakeDriver
	updates             int
	failure, opening    string
	confirmationPrompts int
	dismissed           []string
}

func (d *taskFakeDriver) tasks(string)                        { d.updates++ }
func (d *taskFakeDriver) notify(string, string, string, bool) {}
func (d *taskFakeDriver) dismissNotification(id string)       { d.dismissed = append(d.dismissed, id) }
func (d *taskFakeDriver) notificationStatus() string          { return "authorized" }
func (d *taskFakeDriver) configureNotifications()             {}
func (d *taskFakeDriver) taskFailure(message string)          { d.failure = message }
func (d *taskFakeDriver) taskOpening(id, message string) {
	d.opening = id
	if id != "" && message != "" {
		d.confirmationPrompts++
	}
}

type taskControllerStub struct {
	click   func(context.Context, string) error
	dismiss func(context.Context, string) error
	cancels int
}

func (w *taskControllerStub) Click(ctx context.Context, id string) error   { return w.click(ctx, id) }
func (w *taskControllerStub) Dismiss(ctx context.Context, id string) error { return w.dismiss(ctx, id) }
func (w *taskControllerStub) CancelAll()                                   { w.cancels++ }

func taskService(t *testing.T) (*Service, *taskFakeDriver) {
	t.Helper()
	s := newService(&memoryStore{value: defaults()})
	d := &taskFakeDriver{fakeDriver: &fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}}
	s.start(d)
	t.Cleanup(s.shutdown)
	return s, d
}
func TestTaskBubblesArePassiveAndDispatchOnlyOwnedTarget(t *testing.T) {
	s, d := taskService(t)
	clicks := 0
	w := &taskControllerStub{click: func(_ context.Context, id string) error {
		clicks++
		if id != "owned" {
			t.Fatal("foreign task dispatched")
		}
		return nil
	}}
	s.taskWindows = w
	previews := []api.TaskPreview{{ID: "owned", Prompt: "Private task"}}
	s.observeTasks(previews)
	s.observeTasks(previews)
	if d.updates != 1 || clicks != 0 {
		t.Fatal("passive projection dispatched work")
	}
	if s.openTask(t.Context(), "foreign") == nil || clicks != 0 {
		t.Fatal("unowned task accepted")
	}
	if err := s.openTask(t.Context(), "owned"); err != nil || clicks != 1 {
		t.Fatal(err, clicks)
	}
	s.cancelTerminalOpening()
	if w.cancels != 1 {
		t.Fatal("cancellation not sent to controller")
	}
	s.shutdown()
	if s.openTask(t.Context(), "owned") == nil || clicks != 1 {
		t.Fatal("stopped app dispatched work")
	}
}

func TestTaskUnknownNotificationClearsWhenOriginalStatusRecovers(t *testing.T) {
	s, d := taskService(t)
	s.observeTasks([]api.TaskPreview{{ID: "original", Status: "unknown"}})
	s.observeTasks([]api.TaskPreview{{ID: "original", Status: "working"}})
	s.observeTasks([]api.TaskPreview{{ID: "original", Status: "completed"}})
	if len(d.dismissed) != 1 || d.dismissed[0] != "task-unknown-original" {
		t.Fatal("stale unknown notice remained", d.dismissed)
	}
}
func TestWindowClicksDoNotWaitForOptionalPreview(t *testing.T) {
	s, _ := taskService(t)
	s.observeTasks([]api.TaskPreview{{ID: "owned"}})
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	s.observeTaskTerminal = func(context.Context, string) { close(entered); <-release }
	s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowIdle, State: taskterminal.WindowForeground})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("preview did not start")
	}
	clicks := 0
	s.taskWindows = &taskControllerStub{click: func(context.Context, string) error { clicks++; return nil }}
	done := make(chan error, 1)
	go func() { done <- s.openTask(t.Context(), "owned") }()
	select {
	case err := <-done:
		if err != nil || clicks != 1 {
			t.Fatal(err, clicks)
		}
	case <-time.After(time.Second):
		t.Fatal("preview blocked input")
	}
}
func TestTaskDismissOnlyUnpinsAfterConfirmedClose(t *testing.T) {
	s, d := taskService(t)
	s.observeTasks([]api.TaskPreview{{ID: "owned"}})
	removed, closed := 0, 0
	s.removeTaskPin = func(string) error { removed++; return nil }
	closeErr := errors.New("native close rejected")
	s.taskWindows = &taskControllerStub{dismiss: func(context.Context, string) error { closed++; return closeErr }}
	if err := s.dismissTask(t.Context(), "owned"); err == nil || removed != 0 || d.failure == "" {
		t.Fatal(err, removed)
	}
	closeErr = nil
	if err := s.dismissTask(t.Context(), "owned"); err != nil || removed != 1 {
		t.Fatal(err, removed)
	}
	if err := s.dismissTask(t.Context(), "foreign"); err == nil || closed != 2 {
		t.Fatal("foreign task closed", err)
	}
}
func TestTaskDismissConsentIsNotFailureOrRemoval(t *testing.T) {
	for _, outcome := range []error{taskterminal.ErrWindowCloseCancelled, taskterminal.ErrWindowClosePending, context.Canceled} {
		t.Run(outcome.Error(), func(t *testing.T) {
			s, d := taskService(t)
			s.observeTasks([]api.TaskPreview{{ID: "owned"}})
			removed := false
			s.removeTaskPin = func(string) error { removed = true; return nil }
			s.taskWindows = &taskControllerStub{dismiss: func(context.Context, string) error { return outcome }}
			if err := s.dismissTask(t.Context(), "owned"); err != nil || removed || d.failure != "" {
				t.Fatal(err, removed, d.failure)
			}
		})
	}
}
func TestWindowFailureProjectionKeepsPrivateDetailsOutOfPrompt(t *testing.T) {
	s, d := taskService(t)
	cause := errors.New("private runtime detail")
	reports := 0
	s.taskError = func(_ string, err error) {
		if err != cause {
			t.Fatal(err)
		}
		reports++
	}
	s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowUncertain, Err: cause})
	if reports != 1 || d.failure == "" || d.failure == cause.Error() {
		t.Fatal("private failure projection", reports, d.failure)
	}
}
