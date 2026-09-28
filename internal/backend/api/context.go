package api

import (
	"context"
	"errors"
)

// ErrConversationRenewalRejected means native creation was definitively rejected
// and that result is durable. The source binding is unchanged, so the host may
// retire this handoff attempt and submit to the original conversation normally.
// Unknown outcomes and local persistence failures must not return this error.
var ErrConversationRenewalRejected = errors.New("conversation renewal rejected")

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
	// Host-only assembly versions; never user-facing Session navigation.
	RuntimeVersion, DesiredRuntimeVersion string
	// Observed is true only after native state has been restored. It is independent
	// of Idle: an observed running turn must still invalidate an older handoff.
	Observed bool
	Idle     bool
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
