package api

import "context"

// WorkRuntime is the native execution port consumed by the application's task
// coordinator. It owns native bindings, approvals and mutation receipts, not
// workspace allocation, product capacity or completion-report scheduling.
// All methods are host-only; model input cannot select a workspace or role.
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

// WorkState projects authoritative execution facts. ExecutionKey is an opaque
// native generation, never a product-generated inference from assistant prose.
type WorkState struct {
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
	Runtime, Binary, Endpoint, Thread, Directory, CodexHome string
	// Caelis attaches with the local user credential file; never embed its bytes.
	Session, Store, TokenFile string
}
type TaskPreview struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	Status string `json:"status"`
}

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
