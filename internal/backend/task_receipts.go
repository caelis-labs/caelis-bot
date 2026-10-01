package backend

import "github.com/caelis-labs/caelis-bot/internal/backend/api"

// TaskSummaries exposes bounded product facts for native presentation. It does
// not resolve a receipt, issue a mutation, or export execution bindings/paths.
func (s *Service) TaskSummaries() []api.TaskSummary {
	if source, ok := s.capabilityEngine().(interface{ TaskSummaries() []api.TaskSummary }); ok {
		return source.TaskSummaries()
	}
	workers := s.workerInteractions()
	if workers == nil || workers.owner == nil {
		return []api.TaskSummary{}
	}
	catalog, ok := workers.owner.(interface {
		ListTasks() []api.Task
		TaskPreviews() []api.TaskPreview
	})
	if !ok {
		return []api.TaskSummary{}
	}
	tasks := catalog.ListTasks()
	watchlist := make(map[string]bool)
	for _, preview := range catalog.TaskPreviews() {
		watchlist[preview.ID] = true
	}
	out := make([]api.TaskSummary, 0, len(watchlist))
	for _, task := range tasks {
		if !watchlist[task.ID] {
			continue
		}
		out = append(out, api.TaskSummary{ID: task.ID, Title: task.Title, Status: task.Status, Outcome: task.Outcome, Pinned: true})
		if len(out) >= 512 {
			break
		}
	}
	return out
}
