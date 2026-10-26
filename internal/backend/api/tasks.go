package api

import (
	"context"
	"slices"
)

// TaskProvider is the host-only application coordinator. These handles name
// Bot-owned work, never arbitrary provider conversations or UI sessions.
// The application owns allocation/reporting; native mutation receipts stay in
// WorkRuntime. A protocol adapter does not implement this product interface.
// Start and ordinary Send require an active authorized secretary request. Host-
// validated original question answers retain their own scope while Bot is idle.
// Inputs preserve their source,
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
type TaskWatchlist interface {
	LockTask(string, bool) (TaskSummary, error)
	ClearTasks() error
}
type TaskQuery struct {
	Query  string `json:"query,omitempty"`
	Status string `json:"status,omitempty"`
	Pinned *bool  `json:"pinned,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
type TaskSummary struct {
	Machine     string `json:"machine,omitempty"`
	MachineName string `json:"machineName,omitempty"`
	Locked      bool   `json:"locked"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Outcome     string `json:"outcome,omitempty"`
	Pinned      bool   `json:"pinned"`
}
type TaskPage struct {
	Tasks                     []TaskSummary `json:"tasks"`
	NextCursor                string        `json:"nextCursor,omitempty"`
	Total                     int           `json:"total"`
	Running                   int           `json:"running"`
	Reserved                  int           `json:"reserved"`
	MaxRunning                int           `json:"maxRunning"`
	CompletedRetentionSeconds int           `json:"completedRetentionSeconds"`
}

type Task struct {
	Failure     *TaskFailure `json:"failure,omitempty"`
	CreatedAt   string       `json:"createdAt,omitempty"`
	MachineName string       `json:"machineName,omitempty"`
	Machine     string       `json:"machine,omitempty"`
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Workspace   string       `json:"workspace"`
	Status      string       `json:"status"`
	Outcome     string       `json:"outcome,omitempty"`
	Result      string       `json:"result,omitempty"`
}
type TaskStart struct {
	Attachments []TaskFileReference `json:"attachments,omitempty"`
	Machine     string              `json:"machine,omitempty"`
	// LocalRuntime pins the model-facing "local" choice to the Runtime
	// currently hosting the Bot. Legacy native entry points keep their saved
	// user-configured default; this is never accepted from tool JSON.
	LocalRuntime bool        `json:"-"`
	RequestID    string      `json:"requestId"`
	Title        string      `json:"title"`
	Prompt       string      `json:"prompt"`
	Workspace    string      `json:"workspace,omitempty"`
	Files        []InputFile `json:"-"`
	FileDigests  []string    `json:"fileDigests,omitempty"`
}
type TaskMessage struct {
	Attachments []TaskFileReference `json:"attachments,omitempty"`
	ID          string              `json:"id"`
	RequestID   string              `json:"requestId"`
	Prompt      string              `json:"prompt"`
	Files       []InputFile         `json:"-"`
	FileDigests []string            `json:"fileDigests,omitempty"`
}

// TaskReporter delivers a finite completion notification to the secretary. It
// must never replay an uncertain submission or run a model to poll idle work.
type TaskReporter interface{ DeliverTaskReport(context.Context) error }

// TaskFinisher records the Bot's explicit conclusion after inspecting a result.
// It closes temporary interactions, never stops execution or settles receipts.
type TaskFinisher interface {
	FinishTask(context.Context, string, string) (Task, error)
}

// TaskRequestReader resolves an already-owned original submission without replay.
type TaskRequestReader interface {
	ReadTaskRequest(context.Context, string) (Task, error)
}

// TaskConversationReader reads completed native Worker messages without
// acknowledging a completion notice or changing execution. A missing native
// transcript is an error, never an empty successful conversation.
type TaskConversationReader interface {
	ReadTaskConversation(context.Context, string) (TaskConversation, error)
}

type TaskConversation struct {
	Failure  *TaskFailure              `json:"failure,omitempty"`
	Status   string                    `json:"status"`
	Messages []TaskConversationMessage `json:"messages"`
}
type TaskConversationMessage struct {
	ID        string     `json:"id"`
	Role      string     `json:"role"`
	Text      string     `json:"text"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// TaskHistoryCatalog is the model-facing, creation-time ordered view of all
// retained tasks. It is separate from the desktop watchlist catalog.
type TaskHistoryCatalog interface {
	QueryTaskHistory(TaskHistoryQuery) ([]TaskHistoryEntry, error)
}
type TaskHistoryQuery struct {
	Query, Status, CreatedAfter, CreatedBefore, Order string
	Offset, Limit                                     int
}
type TaskHistoryEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Environment string `json:"environment"`
}

// TaskExecutionSource fixes completion observers to one execution of a reusable task.
// A missing or changed identity must never follow a subsequent continuation.
type TaskExecutionSource interface {
	ObserveTaskExecution(string) (Task, string, error)
}

// TaskFinishToken binds inspection to the original owner/task/native generation.
// It is opaque to the model; reading a different generation invalidates it.
func TaskFinishToken(v WorkState) string {
	if v.ExecutionKey == "" || v.Activity != "idle" || v.StopPending {
		return ""
	}
	switch v.Task.Status {
	case "completed", "failed", "cancelled", "canceled", "interrupted":
	default:
		return ""
	}
	questions := []string{}
	for _, q := range v.AsyncQuestions {
		questions = append(questions, WorkFingerprint(struct {
			ID, Turn, Call string
			Questions      []AsyncQuestion
		}{q.ID, q.TurnKey, q.AsyncCallID, q.AsyncQuestions}))
	}
	slices.Sort(questions)
	return WorkFingerprint(struct {
		Runtime, Task, Machine, Execution, Status, Result string
		Questions                                         []string
	}{v.Runtime, v.Task.ID, v.Task.Machine, v.ExecutionKey, v.Task.Status, v.Task.Result, questions})
}
