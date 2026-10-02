package desktop

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"testing"
)

func TestCloseConfirmationProjectionAndCancellation(t *testing.T) {
	s, d := taskService(t)
	s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowClosing, State: taskterminal.WindowForeground})
	if d.opening != "owned" || d.confirmationPrompts != 1 {
		t.Fatal("missing close confirmation hint", d)
	}
	s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 2, Phase: taskterminal.WindowIdle, State: taskterminal.WindowForeground, Err: taskterminal.ErrWindowCloseCancelled})
	if d.failure != "" || d.opening != "" {
		t.Fatal("cancel displayed as error or left prompt", d)
	}
}

func TestOpenCancellationOrMissingReceiptClearsWaiting(t *testing.T) {
	for _, err := range []error{taskterminal.ErrWindowOpenCancelled, taskterminal.ErrWindowNotConnected} {
		s, d := taskService(t)
		s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowOpening})
		s.taskWindowChanged("owned", taskterminal.WindowEvent{Revision: 2, Phase: taskterminal.WindowIdle, Err: err})
		if d.opening != "" {
			t.Fatal("waiting hint survived", err)
		}
		if err == taskterminal.ErrWindowOpenCancelled && d.failure != "" {
			t.Fatal("cancel reported failure")
		}
		if err == taskterminal.ErrWindowNotConnected && d.failure != s.text("native.taskTerminalNotConnected", nil) {
			t.Fatal("missing retry feedback", d.failure)
		}
	}
}

type transitionDriver struct {
	taskFakeDriver
	transitions map[string]string
}

func (d *transitionDriver) taskTransitions(data string) {
	d.transitions = nil
	_ = json.Unmarshal([]byte(data), &d.transitions)
}
func TestTaskProjectionFencesStaleCompletionAndKeepsOtherTaskBusy(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &transitionDriver{taskFakeDriver: taskFakeDriver{fakeDriver: &fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}}}
	s.start(d)
	defer s.shutdown()
	s.taskWindowChanged("a", taskterminal.WindowEvent{Revision: 2, Phase: taskterminal.WindowShowing})
	s.taskWindowChanged("b", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowOpening})
	s.taskWindowChanged("a", taskterminal.WindowEvent{Revision: 1, Phase: taskterminal.WindowIdle})
	if d.transitions["a"] != "showing" || d.transitions["b"] != "opening" {
		t.Fatal(d.transitions)
	}
	s.taskWindowChanged("a", taskterminal.WindowEvent{Revision: 3, Phase: taskterminal.WindowIdle})
	if d.transitions["a"] != "" || d.transitions["b"] != "opening" {
		t.Fatal(d.transitions)
	}
}
func TestUnsettledWindowIsNotMisreportedAsPermissionDenied(t *testing.T) {
	if key := taskWindowErrorKey(taskterminal.ErrWindowClosePending); key != "native.taskTerminalClosePending" {
		t.Fatal(key)
	}
	if key := taskWindowErrorKey(taskterminal.ErrWindowUnsettled); key != "native.taskTerminalUnsettled" {
		t.Fatal(key)
	}
	if key := taskWindowErrorKey(taskterminal.ErrWindowPermission); key != "native.taskTerminalPermission" {
		t.Fatal(key)
	}
}
