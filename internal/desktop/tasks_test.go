package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type taskFakeDriver struct {
	*fakeDriver
	updates int
	failure string
	opening string
}

func (d *taskFakeDriver) tasks(string)                   { d.updates++ }
func (d *taskFakeDriver) taskFailure(message string)     { d.failure = message }
func (d *taskFakeDriver) taskOpening(id, message string) { d.opening = id }

func TestTaskBubblesArePassiveAndLaunchOnlyOwnedTarget(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &taskFakeDriver{fakeDriver: &fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}}
	s.start(d)
	defer s.shutdown()
	resolves, launches, logs := 0, 0, 0
	s.resolveTaskTerminal = func(_ context.Context, id string) (api.TerminalTarget, error) {
		resolves++
		if id != "owned" {
			t.Fatal("foreign target resolved")
		}
		return api.TerminalTarget{Thread: "native-owned"}, nil
	}
	s.launchTaskTerminal = func(_ context.Context, id string, target api.TerminalTarget) error {
		launches++
		if target.Thread != "native-owned" {
			t.Fatal("wrong target")
		}
		return nil
	}
	s.taskError = func(string, error) { logs++ }
	previews := []api.TaskPreview{{ID: "owned", Prompt: "Original prompt\nwith details"}}
	s.observeTasks(previews)
	s.observeTasks(previews)
	if d.updates != 1 || resolves != 0 || launches != 0 {
		t.Fatal("projection polled/launched backend")
	}
	if s.openTask(context.Background(), "foreign") == nil || resolves != 0 {
		t.Fatal("unowned task accepted")
	}
	if err := s.openTask(context.Background(), "owned"); err != nil || launches != 1 {
		t.Fatal("click failed", err)
	}
	s.launchTaskTerminal = func(context.Context, string, api.TerminalTarget) error { return errors.New("private runtime detail") }
	if s.openTask(context.Background(), "owned") == nil || logs != 1 || d.failure == "" || d.failure == "private runtime detail" {
		t.Fatal("launch failure not privately diagnosed")
	}
	s.shutdown()
	if s.openTask(context.Background(), "owned") == nil || resolves != 2 {
		t.Fatal("closed app launched a task")
	}
}

func TestTerminalConfirmationCoalescesClicksAndCanBeCanceled(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	d := &taskFakeDriver{fakeDriver: &fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}}
	s.start(d)
	defer s.shutdown()
	s.observeTasks([]api.TaskPreview{{ID: "owned"}})
	s.resolveTaskTerminal = func(context.Context, string) (api.TerminalTarget, error) { return api.TerminalTarget{}, nil }
	entered := make(chan struct{})
	done := make(chan error, 1)
	launches := 0
	s.launchTaskTerminal = func(ctx context.Context, _ string, _ api.TerminalTarget) error {
		launches++
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	go func() { done <- s.openTask(t.Context(), "owned") }()
	<-entered
	s.mu.Lock()
	opening := d.opening
	s.mu.Unlock()
	if opening != "owned" {
		t.Fatal("pending launch not visible")
	}
	// Waiting belongs to this UI launch only, not the task projection or Bot.
	s.observeTasks([]api.TaskPreview{{ID: "owned", Status: "completed"}})
	if d.updates != 2 {
		t.Fatal("terminal consent blocked task updates")
	}
	if err := s.openTask(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	s.cancelTerminalOpening()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if launches != 1 || d.opening != "" || d.failure == "" {
		t.Fatal("opening state not cleared", launches, d.opening, d.failure)
	}
}
