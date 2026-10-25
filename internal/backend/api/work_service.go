package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// WorkerContract is independent of the browser surface and the
// renderer contract. Missing capabilities fail closed; they never select a new
// execution owner or fall back to the local machine.
const WorkerContract = 1

var ErrWorkOperationUnsupported = errors.New("worker_reconcile_unsupported")

// WorkCapabilities describes the actual native owner, including retained owners.
// Owner and Build must not be replaced with the currently running App identity.
type WorkCapabilities struct {
	Contract             int    `json:"contract"`
	Owner                string `json:"owner"`
	Build                string `json:"build"`
	Runtime              string `json:"runtime"`
	History              bool   `json:"history"`
	Observe              bool   `json:"observe"`
	Interactions         bool   `json:"interactions"`
	Files                bool   `json:"files"`
	InputMaxFiles        int    `json:"inputMaxFiles"`
	InputMaxFileBytes    int64  `json:"inputMaxFileBytes"`
	Reconcile            bool   `json:"reconcile"`
	URL                  bool   `json:"url"`
	PendingAcrossRestart bool   `json:"pendingAcrossRestart"`
}
type WorkCapabilityProvider interface {
	WorkCapabilities(context.Context, string) (WorkCapabilities, error)
}

// WorkObservation is a bounded current snapshot, never a permanent event log.
// Items include live native partial output. Completed history is read separately.
// The peer wire projection explicitly includes the host-only question fields.
type WorkObservation struct {
	State     WorkState
	Approvals []Approval
	Items     []Item
	Revision  string
	Owner     WorkCapabilities
}
type WorkObserver interface {
	ObserveWork(context.Context, string) (WorkObservation, error)
}

// WorkStop is checked under the adapter's native operation lock immediately
// before interrupting the exact native target. ExecutionKey comes from the
// adapter, not from task status or a turn inferred by the host.
type WorkStop struct {
	ID           string `json:"id"`
	RequestID    string `json:"requestId"`
	ExecutionKey string `json:"executionKey"`
}
type WorkStopper interface {
	StopWorkRequest(context.Context, WorkStop) (Task, error)
}

// Decision.ID always remains the original native request ID. TaskID scopes it
// to its source binding. RequestID identifies this one host decision operation.
// A pending request may outlive a turn; do not expire it on a turn-key change.
type WorkDecision struct {
	TaskID    string   `json:"taskId"`
	RequestID string   `json:"requestId"`
	Decision  Decision `json:"decision"`
}

// WorkDecisionRequestID stays fixed for the exact user intent. A corrected known
// validation refusal can be a new intent; adapters fence an unresolved prior
// native decision so edited answers cannot bypass an uncertain write.
func WorkDecisionRequestID(task string, decision Decision) string {
	return "decision-" + WorkFingerprint(struct {
		Task     string
		Decision Decision
	}{task, decision})
}

type WorkDecider interface {
	DecideWork(context.Context, WorkDecision) error
	WorkApprovalURL(context.Context, string, string) (string, error)
}

// WorkOperation reads an original receipt. unknown is never permission to replay.
// Adapters extend their native binding/receipt records; this port is not a ledger.
type WorkOperationQuery struct {
	TaskID      string `json:"taskId"`
	RequestID   string `json:"requestId"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
}
type WorkOperation struct {
	WorkOperationQuery
	Outcome string `json:"outcome"` // accepted, rejected, unknown
}
type WorkOperationReader interface {
	ReadWorkOperation(context.Context, WorkOperationQuery) (WorkOperation, error)
}

// WorkFingerprint hashes the exact host intent without retaining secret answer
// text. The same operation ID with a different digest is a conflict.
func WorkFingerprint(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// WorkInputDigests binds input bytes and names to an operation, without binding
// machine-local staging paths. Callers compare supplied digests before dispatch.
func WorkInputDigests(files []InputFile) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > 8 {
		return nil, errors.New("too_many_worker_files")
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		f, err := os.Open(file.Path)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, MaxWorkFileBytes+1))
		_ = f.Close()
		if err != nil || len(data) > MaxWorkFileBytes {
			return nil, errors.New("worker_file_unavailable_or_too_large")
		}
		sum := sha256.Sum256(data)
		out = append(out, WorkFingerprint(struct{ Name, SHA256 string }{file.Name, hex.EncodeToString(sum[:])}))
	}
	return out, nil
}

// WorkFileReader resolves only artifacts proven to belong to this native task.
// Paths never cross machines. Inputs use WorkStart.Files / TaskMessage.Files and
// the same native encoders as local submissions, not the resident upload API.
const MaxWorkFileBytes = 20 << 20

type WorkFileContent struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      []byte `json:"data"`
}
type WorkFileReader interface {
	ReadWorkFile(context.Context, string, string) (WorkFileContent, error)
}

// Validation reads only the prepared input bytes under the original route.
// It creates no native task, request, operation or approval. Adapters retain
// their normal dispatch-boundary validation as well.
type WorkInputValidator interface {
	ValidateWorkFiles(context.Context, string, []InputFile) error
}
