package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"slices"
)

func (s *Service) ConfigureWorkerTaskView(v api.WorkerTaskViewer) { s.workerView = v }
func (s *Service) WorkerTasks() []api.Task {
	if s.workerView == nil {
		return []api.Task{}
	}
	tasks := slices.Clone(s.workerView.ListTasks())
	if s.machines != nil {
		names := map[string]string{}
		for _, machine := range s.machines.Machines() {
			names[machine.ID] = machine.Name
		}
		for i := range tasks {
			if name := names[tasks[i].Machine]; name != "" {
				tasks[i].MachineName = name
			}
		}
	}
	return tasks
}
func (s *Service) WorkerTaskConversation(ctx context.Context, id string) (api.TaskConversation, error) {
	if s.workerView == nil {
		return api.TaskConversation{}, errors.New("worker_view_unavailable")
	}
	return s.workerView.ReadTaskConversation(ctx, id)
}
func (s *Service) WorkerTaskObservation(ctx context.Context, id string) (api.WorkerTaskObservation, error) {
	if s.workerView == nil {
		return api.WorkerTaskObservation{}, errors.New("worker_view_unavailable")
	}
	v, err := s.workerView.ObserveTask(ctx, id)
	if v.Items == nil {
		v.Items = []api.Item{}
	}
	return api.WorkerTaskObservation{Task: v.State.Task, Items: v.Items, Revision: v.Revision}, err
}
