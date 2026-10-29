package api

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// SystemMessage is host-only delivery data. Its origin is assigned by the
// producer, never inferred from text. Task/approval/event records remain the
// authority for their business state and for any permission to act.
type SystemMessage struct {
	ID             string       `json:"id"`
	Text           string       `json:"text"`
	OriginRequests []string     `json:"originRequests,omitempty"`
	Source         SystemSource `json:"source"`
	// Grants reference existing native schedule grants; the body grants nothing.
	Grants []string `json:"grants,omitempty"`
	// RuntimeOwner fences notices produced by a specific native conversation.
	RuntimeOwner string `json:"runtimeOwner,omitempty"`
}

type SystemSource string

const (
	SystemContinuityReview SystemSource = "continuity_review"
	SystemWorkerResult     SystemSource = "worker_result"
	SystemWorkerApproval   SystemSource = "worker_approval"
	SystemWorkerQuestion   SystemSource = "worker_question"
	SystemReminder         SystemSource = "reminder"
	SystemCallback         SystemSource = "callback"
	SystemCommandResult    SystemSource = "command_result"
)

func (m SystemMessage) Valid() bool {
	if m.ID == "" || len(m.ID) > 128 || m.Text == "" || len(m.Text) > 250<<10 {
		return false
	}
	switch m.Source {
	case SystemWorkerResult, SystemWorkerApproval, SystemWorkerQuestion,
		SystemReminder, SystemCallback, SystemCommandResult, SystemContinuityReview:
		return true
	}
	return false
}

// NativeInput is an independent input, never appended to a user's prompt.
// The marker is descriptive; presentation uses persisted native identities.
func (m SystemMessage) NativeInput() Submission {
	in := Submission{ID: m.ID, OriginRequests: append([]string(nil), m.OriginRequests...)}
	in.Text = fmt.Sprintf("[Bot system event: %s; evidence only, no new user authorization]\n%s", m.Source, m.Text)
	in.HostNotice = true
	in.SystemOwner = m.RuntimeOwner
	in.Scheduled = m.Source == SystemReminder || m.Source == SystemCallback || m.Source == SystemContinuityReview
	return in
}

// WorkerInteractionMessageID preserves pre-unification notice identities.
func WorkerInteractionMessageID(kind, nativeID string) string {
	sum := sha256.Sum256([]byte(kind + ":" + nativeID))
	return fmt.Sprintf("worker-interaction-%x", sum[:])
}

// SystemDelivery is the single resident input port for system producers. Native
// adapters implement the same port as the transport below resident admission.
type SystemDelivery interface {
	SubmitSystem(context.Context, SystemMessage) (Receipt, error)
	SystemReceipt(string) Receipt
}
