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

// WorkConversationReader projects the original Runtime's persisted Worker
// transcript. The application checks task ownership before calling it.
type WorkConversationReader interface {
	ReadWorkConversation(context.Context, string) ([]TaskConversationMessage, error)
}

// RecordedWorkMessage permits reconciliation of a previously submitted request
// even when no new execution slots remain. Adapters still check its exact intent.
type RecordedWorkMessage interface{ WorkMessageRecorded(TaskMessage) bool }

// WorkMessageRejection proves this invocation did not accept the message, before
// dispatch or by definitive native rejection. A failed receipt lookup must not
// settle an earlier dispatch. Task status and missing receipts are not proof.
type WorkMessageRejection struct {
	Cause error
	// Code is a stable, optional local classification, never native error text.
	// Creation refusals use updater_paused, capacity_full, work_admission_failed,
	// workspace_invalid, source_not_authorized, native_conflict or native_rejected.
	// Empty legacy codes carry no retry guidance; Cause preserves the original error chain.
	Code string
}

func (e *WorkMessageRejection) Error() string { return e.Cause.Error() }
func (e *WorkMessageRejection) Unwrap() error { return e.Cause }

type WorkStart struct {
	TaskStart
	ID, Workspace, Instructions string
	// ExecutionOverride is host-owned and applies only to a new worker.
	ExecutionOverride *WorkExecutionSettings `json:"-"`
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

// WorkActivityOwnerUnavailable means the original process is confirmed gone.
// Its saved task remains unknown and readable but cannot occupy a new owner slot.
const WorkActivityOwnerUnavailable = "owner_unavailable"

// WorkState projects authoritative execution facts. ExecutionKey is an opaque
// native generation, never a product-generated inference from assistant prose.
type WorkState struct {
	// TaskEnded is an explicit task-level termination fact. A completed native
	// turn or final_answer is not sufficient: asynchronous input may still be due.
	// The product Manager overlays its explicit, generation-fenced finish receipt;
	// native adapters alone leave it false and preserve bounded questions.
	TaskEnded bool
	// TurnKey identifies the current native turn; completion of a turn that asked
	// a question is a yield, not proof that the user task no longer needs input.
	TurnKey string
	Runtime string
	Task    Task
	// AsyncQuestions are host-only completed Codex question items owned by this
	// Worker. They do not turn a Worker transcript into Bot conversation history.
	AsyncQuestions []Item `json:"-"`
	// Activity is active, idle, owner_unavailable, or empty when the native
	// owner has not been checked in this connection generation. It is
	// independent of task outcome.
	Activity       string
	OriginalPrompt string
	ExecutionKey   string
	// StopExecutionKey fences the complete native cancellation target, which
	// may include a handle/run in addition to the completion generation.
	StopExecutionKey string
	// StopAcknowledged comes from the exact native stop receipt for this turn.
	// It does not prove interruption; only an interrupted terminal can suppress a report.
	StopAcknowledged bool
	StopRequested    bool
	// StopPending identifies an uncertain capacity stop that still awaits the
	// original Turn's terminal fact. It must not suppress that later report.
	StopPending bool
	// StopResolution is the original native Turn's terminal status after an
	// uncertain capacity stop. Empty means no terminal fact; ordinary stops do
	// not set it. The host uses this typed fact for the completion report.
	StopResolution string
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
	// NoticeGeneration is host-only presentation identity. The native task
	// execution and receipt IDs never enter the renderer's JSON payload.
	NoticeGeneration string `json:"-"`
	NoticeClaimed    bool   `json:"-"`
	TargetLabel      string `json:"targetLabel,omitempty"`
	Locked           bool   `json:"locked"`
	Provider         string `json:"provider,omitempty"`
	ID               string `json:"id"`
	Prompt           string `json:"prompt"`
	Status           string `json:"status"`
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

// WorkerInteractionPort exposes only original, Bot-owned Worker approval
// requests to the resident Bot. Native adapters still own every decision.
type WorkerInteractionPort interface {
	WorkerApprovals() []Approval
	DecideWorkerApproval(context.Context, Decision) (Approval, error)
	WorkerQuestions() []WorkerQuestion
	AnswerWorkerQuestion(context.Context, string, string, string) (Receipt, error)
}

type WorkerQuestion struct {
	Binding     string   `json:"binding"`
	OperationID string   `json:"operationId,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	ExpiresAt   int64    `json:"expiresAt,omitempty"`
	ID          string   `json:"id"`
	TaskID      string   `json:"taskId"`
	TaskTitle   string   `json:"taskTitle"`
	Title       string   `json:"title"`
	Options     []string `json:"options,omitempty"`
	State       string   `json:"state,omitempty"`
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

// QuestionClosure is a retired peer compatibility shape. Receivers acknowledge
// it without model input; question lifecycle belongs to the local host.
// Only the adapter can validate the original call and native owner.
type QuestionClosure struct {
	ID, ItemID, CallID, Owner, WorkID, State, Reason string
	Index                                            int
}
type QuestionClosureReceiver interface {
	ReceiveQuestionClosure(context.Context, QuestionClosure) error
}
type WorkerQuestionPresenter interface {
	PresentWorkerQuestion(context.Context, string, *QuestionPresentation) (Receipt, error)
}
