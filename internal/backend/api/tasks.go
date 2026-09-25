package api

import "context"

// TaskProvider is the host-only application coordinator. These handles name
// Bot-owned work, never arbitrary provider conversations or UI sessions.
// The application owns allocation/reporting; native mutation receipts stay in
// WorkRuntime. A protocol adapter does not implement this product interface.
// Start/Send require an active authorized secretary request, preserve its source,
// and reject conflicting request IDs. An unknown outcome is not a retry grant.
// Start validates an explicit workspace or allocates a private directory.
// Stop addresses the exact active turn; read/list never adopt unrelated tasks.
type TaskProvider interface {
	ListTasks() []Task
	StartTask(context.Context, TaskStart) (Task, error)
	ReadTask(context.Context, string) (Task, error)
	SendTask(context.Context, TaskMessage) (Task, error)
	StopTask(context.Context, string) (Task, error)
}

// TaskCatalog manages the product watchlist independently of execution.
type TaskCatalog interface {
	QueryTasks(TaskQuery) (TaskPage, error)
	PinTask(string, bool) (TaskSummary, error)
}
type TaskQuery struct {
	Query  string `json:"query,omitempty"`
	Status string `json:"status,omitempty"`
	Pinned *bool  `json:"pinned,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
type TaskSummary struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Outcome string `json:"outcome,omitempty"`
	Pinned  bool   `json:"pinned"`
}
type TaskPage struct {
	Tasks       []TaskSummary `json:"tasks"`
	NextCursor  string        `json:"nextCursor,omitempty"`
	Total       int           `json:"total"`
	Running     int           `json:"running"`
	MaxRunning  int           `json:"maxRunning"`
	PinnedLimit int           `json:"pinnedLimit"`
}

type Task struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	Status    string `json:"status"`
	Outcome   string `json:"outcome,omitempty"`
	Result    string `json:"result,omitempty"`
}
type TaskStart struct {
	RequestID string `json:"requestId"`
	Title     string `json:"title"`
	Prompt    string `json:"prompt"`
	Workspace string `json:"workspace,omitempty"`
}
type TaskMessage struct {
	ID        string `json:"id"`
	RequestID string `json:"requestId"`
	Prompt    string `json:"prompt"`
}

// TaskReporter delivers a finite completion notification to the secretary. It
// must never replay an uncertain submission or run a model to poll idle work.
type TaskReporter interface{ DeliverTaskReport(context.Context) error }
