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

type WorkStart struct {
	TaskStart
	ID, Workspace, Instructions string
}

// WorkState projects authoritative execution facts. ExecutionKey is an opaque
// native generation, never a product-generated inference from assistant prose.
type WorkState struct {
	Task          Task
	ExecutionKey  string
	StopRequested bool
	// Existing native bindings may contain a completion receipt from before the
	// product coordinator existed. Import it once, so upgrading cannot re-report.
	PreviousReportID, PreviousReportState string
	StartFingerprint                      string
}

// ReportSubmitter appends a bounded application notice only when idle. It must
// not promote that notice into a new user request or delegation authority.
type ReportSubmitter interface {
	SubmitReport(context.Context, Submission) (Receipt, error)
}
