package api

import (
	"context"
	"time"
)

// WorkRuntime is the native execution port consumed by the application's task
// coordinator. It owns native bindings, approvals and mutation receipts, not
// workspace allocation, product capacity or completion-report scheduling.
// All methods are host-only; the host validates the workspace and owns the role.
type WorkRuntime interface {
	WorkAdmission(context.Context) error
	WorkStates() []WorkState
	StartWork(context.Context, WorkStart) (Task, error)
	ReadWork(context.Context, string) (Task, error)
	SendWork(context.Context, TaskMessage) (Task, error)
	StopWork(context.Context, string) (Task, error)
}

// RecordedWorkMessage permits reconciliation of a previously submitted request
// even when no new execution slots remain. Adapters still check its exact intent.
type RecordedWorkMessage interface{ WorkMessageRecorded(TaskMessage) bool }

type WorkStart struct {
	TaskStart
	ID, Workspace, Instructions string
}

// WorkRouter freezes the native backend before preparation or dispatch. An empty
// runtime resolves the current default; a nonempty runtime restores a ledger
// binding. This is a host port, never a model-selectable task parameter.
type WorkRouter interface {
	BindWork(context.Context, TaskStart, string, string) (string, error)
	OwnsWork(string) bool
}

// WorkPreparationRollback releases only a reservation that has never attempted
// Worker dispatch. It must not discard unknown execution owners.
type WorkPreparationRollback interface {
	ReleaseWorkPreparation(string) error
}

type LocalWorkerSettings struct {
	Runtime        string                 `json:"runtime"`
	Ready          bool                   `json:"ready"`
	Work           WorkExecutionSettings  `json:"work"`
	Models         []ModelOption          `json:"models"`
	RuntimeDefault *WorkExecutionSettings `json:"runtimeDefault"`
}
type LocalWorkerController interface {
	InspectLocalWorker(context.Context, string) (LocalWorkerSettings, error)
	SaveLocalWorkerModel(context.Context, WorkExecutionSettings) (LocalWorkerSettings, error)
}

// WorkState projects authoritative execution facts. ExecutionKey is an opaque
// native generation, never a product-generated inference from assistant prose.
type WorkState struct {
	Runtime        string
	Task           Task
	OriginalPrompt string
	ExecutionKey   string
	StopRequested  bool
	// Existing native bindings may contain a completion receipt from before the
	// product coordinator existed. Import it once, so upgrading cannot re-report.
	PreviousReportID, PreviousReportState string
	StartFingerprint                      string
}

// WorkTerminalProvider exposes an owned native target to the host, never a shell
// command supplied by the model or renderer. Resolving does not start a turn.
type WorkTerminalProvider interface {
	WorkTerminal(context.Context, string) (TerminalTarget, error)
}
type TerminalTarget struct {
	// SSH arguments are assembled by the native connection owner, never the renderer.
	SSH                                                     []string
	Runtime, Binary, Endpoint, Thread, Directory, CodexHome string
	// Caelis attaches with the local user credential file; never embed its bytes.
	Session, Store, TokenFile string
}
type TaskPreview struct {
	TargetLabel string `json:"targetLabel,omitempty"`
	Locked      bool   `json:"locked"`
	Provider    string `json:"provider,omitempty"`
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Status      string `json:"status"`
}

// RemoteWorkspaceRuntime resolves a target path without evaluating it locally.
type RemoteWorkspaceRuntime interface {
	PrepareRemoteWork(context.Context, TaskStart, string) (string, error)
}
type TaskMachine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	Ready   bool   `json:"ready"`
}
type TaskMachineProvider interface{ TaskMachines() []TaskMachine }

// ReportSubmitter appends a bounded application notice only when idle. It must
// not promote that notice into a new user request or delegation authority.
type ReportSubmitter interface {
	SubmitReport(context.Context, Submission) (Receipt, error)
}

// BackgroundRuntime keeps timer provenance separate from actual user messages.
// Implementations attest authorization when the user creates/updates a schedule.
type BackgroundRuntime interface {
	AuthorizeBackground(context.Context, string, string) error
	RevokeBackground(context.Context, string) error
	SubmitBackground(context.Context, Submission, []string) (Receipt, error)
}

// BackgroundReceiptProvider reads retained native submission evidence without
// dispatching again, even after later user input replaces LastReceipt.
type BackgroundReceiptProvider interface{ BackgroundReceipt(string) Receipt }

// BackgroundResult is retained presentation evidence for one native submission.
// ObservedAt is the host's first observation, not a fabricated native timestamp.
// It carries no text or execution authority and never means the user read it.
type BackgroundResult struct {
	ID         string    `json:"id"`
	Complete   bool      `json:"complete"`
	Visible    bool      `json:"visible"`
	ObservedAt time.Time `json:"observedAt,omitempty"`
}
type BackgroundResultProvider interface{ BackgroundResult(string) BackgroundResult }
