package backend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerViewFixture struct {
	observation api.WorkObservation
	tasks       []api.Task
}

func (f workerViewFixture) ListTasks() []api.Task { return f.tasks }
func (f workerViewFixture) ReadTaskConversation(context.Context, string) (api.TaskConversation, error) {
	return api.TaskConversation{}, nil
}
func (f workerViewFixture) ObserveTask(context.Context, string) (api.WorkObservation, error) {
	return f.observation, nil
}

func TestWorkerTaskObservationProjectsEmptyItemsAsArray(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	s.ConfigureWorkerTaskView(workerViewFixture{observation: api.WorkObservation{
		State: api.WorkState{Task: api.Task{ID: "original-task", Status: "running"}},
	}})
	v, err := s.WorkerTaskObservation(t.Context(), "original-task")
	if err != nil || v.Items == nil || len(v.Items) != 0 {
		t.Fatalf("empty output projection: %+v %v", v, err)
	}
	data, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(data), `"items":[]`) {
		t.Fatalf("renderer items must be an array: %s %v", data, err)
	}
}

type workerMachineNames struct {
	api.MachineController
	values []api.Machine
}

func (f workerMachineNames) Machines() []api.Machine { return f.values }
func TestWorkerTaskCatalogProjectsCurrentEnvironmentWithoutRewritingLedger(t *testing.T) {
	s := NewService(nil, nil, nil, nil, nil)
	original := []api.Task{{ID: "original", Machine: "peer-id", MachineName: "http://old-address", Status: "completed"}, {ID: "local", Status: "running"}}
	s.ConfigureWorkerTaskView(workerViewFixture{tasks: original})
	s.machines = workerMachineNames{values: []api.Machine{{ID: "peer-id", Name: "fedora", Archived: true}}}
	got := s.WorkerTasks()
	if got[0].MachineName != "fedora" || got[0].Machine != "peer-id" || got[0].Status != "completed" || got[1] != original[1] || original[0].MachineName != "http://old-address" {
		t.Fatal("view changed ownership or rewrote ledger", got, original)
	}
}
