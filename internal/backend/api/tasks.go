package api

import "context"

// TaskProvider is an optional, host-only backend capability. These handles name
// Bot-owned work, never arbitrary provider conversations or UI sessions.
// Ownership and native mutation receipts belong to the adapter/Control owner.
// Start/Send require an active authorized secretary request, preserve its source,
// and reject conflicting request IDs. An unknown outcome is not a retry grant.
// Start allocates a private workspace; it never trusts a model-supplied path.
// Stop addresses the exact active turn; read/list never adopt unrelated tasks.
type TaskProvider interface {
	ListTasks() []Task
	StartTask(context.Context, TaskStart) (Task, error)
	ReadTask(context.Context, string) (Task, error)
	SendTask(context.Context, TaskMessage) (Task, error)
	StopTask(context.Context, string) (Task, error)
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
}
type TaskMessage struct {
	ID        string `json:"id"`
	RequestID string `json:"requestId"`
	Prompt    string `json:"prompt"`
}

// TaskReporter delivers a finite completion notification to the secretary. It
// must never replay an uncertain submission or run a model to poll idle work.
type TaskReporter interface{ DeliverTaskReport(context.Context) error }
