package api

import "context"

// ContextSeed is application-owned data appended at the first request boundary,
// never policy or an instruction override. HandoffDigest permits conditional
// consumption only after native acceptance has been durably recorded.
type ContextSeed struct {
	Text          string `json:"text,omitempty"`
	HandoffDigest string `json:"handoffDigest,omitempty"`
}

// ConversationState describes only the resident conversation. Worker activity
// cannot be mistaken for another resident turn or a successful Dream.
type ConversationState struct {
	Session, Turn, Status string
	Idle                  bool
}

// ConversationRuntime performs ordinary native session/turn operations. It has
// no provider-compaction operation; the Bot owns when to dream and rotate.
type ConversationRuntime interface {
	ConversationState() ConversationState
	SubmitDream(context.Context, Submission) (Receipt, error)
	DreamResult(string) (Receipt, ConversationState)
	CancelDream(context.Context, string) error
	RenewConversation(context.Context, string, string) error
}
