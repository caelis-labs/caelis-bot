package backend

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

type receiptCatalogFixture struct {
	workerRouteFixture
	tasks    []api.Task
	previews []api.TaskPreview
}

func (f *receiptCatalogFixture) ListTasks() []api.Task           { return f.tasks }
func (f *receiptCatalogFixture) TaskPreviews() []api.TaskPreview { return f.previews }

func TestTaskReceiptProjectionRetainsTerminalUnknownAndCurrentWatchlist(t *testing.T) {
	service := NewService(nil, nil, nil, nil, nil)
	owner := &receiptCatalogFixture{tasks: []api.Task{{ID: "owned", Title: "Task", Workspace: "private-workspace", Status: "interrupted", Outcome: "unknown", Result: "private-result"}, {ID: "old", Status: "completed", Outcome: "accepted"}}, previews: []api.TaskPreview{{ID: "owned", Status: "interrupted"}}}
	service.ConfigureWorkRoutes(owner, t.TempDir())
	rows := service.TaskSummaries()
	if len(rows) != 1 || rows[0].ID != "owned" || rows[0].Status != "interrupted" || rows[0].Outcome != "unknown" || !rows[0].Pinned {
		t.Fatal("receipt uncertainty/current watchlist lost", rows)
	}
}
