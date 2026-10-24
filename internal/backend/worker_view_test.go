package backend

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerViewFixture struct{ observation api.WorkObservation }

func (f workerViewFixture) ListTasks() []api.Task { return nil }
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
