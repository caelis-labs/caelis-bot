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
}

func (d *taskFakeDriver) tasks(string)               { d.updates++ }
func (d *taskFakeDriver) taskFailure(message string) { d.failure = message }

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
