package desktop

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestSharedWorkerViewerUsesOnlyOwnedCardsAndKeepsLegacyFallback(t *testing.T) {
	s, _ := taskService(t)
	s.observeTasks([]api.TaskPreview{{ID: "local"}, {ID: "bot-peer"}, {ID: "legacy-ssh"}})
	viewed := []string{}
	terminal := []string{}
	s.workerTaskView = func(_ context.Context, id string) (bool, error) {
		if id == "legacy-ssh" {
			return false, nil
		}
		viewed = append(viewed, id)
		return true, nil
	}
	s.taskWindows = &taskControllerStub{click: func(_ context.Context, id string) error { terminal = append(terminal, id); return nil }}
	for _, id := range []string{"local", "bot-peer", "legacy-ssh"} {
		if err := s.OpenTaskTerminal(id); err != nil {
			t.Fatal(err)
		}
	}
	if len(viewed) != 2 || viewed[0] != "local" || viewed[1] != "bot-peer" || len(terminal) != 1 || terminal[0] != "legacy-ssh" {
		t.Fatal("local/peer view diverged or legacy helper was rerouted")
	}
	if s.OpenTaskTerminal("foreign") == nil {
		t.Fatal("unowned task opened")
	}
	s.workerTaskSelection = "local"
	if s.WorkerTaskSelection() != "local" {
		t.Fatal("selection lost")
	}
	s.CloseWorkerTask()
	if s.WorkerTaskSelection() != "" {
		t.Fatal("view close changed selection incorrectly")
	}
}
