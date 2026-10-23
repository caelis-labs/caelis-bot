package api

import "context"

// WorkURLChallenge is a user-only companion to WorkRuntime, not a native
// approval or a general HTTP port. The owner derives every target and hidden
// value from the original request. No URL or destination is accepted from A.
// Native consent remains an independent WorkDecider operation.
type WorkURLChallenge interface {
	ReadWorkURL(context.Context, string, string) (WorkURLPage, error)
	SubmitWorkURL(context.Context, WorkURLSubmission) (WorkOperation, error)
	ReadWorkURLSubmission(context.Context, string, string, string) (WorkOperation, error)
}

type WorkURLPage struct {
	Revision    string          `json:"revision"`
	Origin      string          `json:"origin"`
	Title       string          `json:"title"`
	Text        string          `json:"text"`
	SubmitLabel string          `json:"submitLabel"`
	Fields      []WorkURLField  `json:"fields"`
	Operation   *WorkURLReceipt `json:"operation,omitempty"`
}
type WorkURLField struct {
	Name     string          `json:"name"`
	Label    string          `json:"label"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Value    string          `json:"value"`
	Options  []WorkURLOption `json:"options"`
}
type WorkURLOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type WorkURLSubmission struct {
	TaskID     string            `json:"taskId"`
	ApprovalID string            `json:"approvalId"`
	RequestID  string            `json:"requestId"`
	Revision   string            `json:"revision"`
	Values     map[string]string `json:"values"`
}

const WorkURLSubmitKind = "url-submit"

// User-facing receipt contains no entered values or native response payload.
type WorkURLReceipt struct {
	RequestID   string `json:"requestId"`
	Fingerprint string `json:"fingerprint"`
	Outcome     string `json:"outcome"`
}
