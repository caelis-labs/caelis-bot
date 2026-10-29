package api

import (
	"context"
	"errors"
	"time"
)

// ErrConversationRenewalRejected means native creation was definitively rejected
// and that result is durable. The source binding is unchanged, so the host may
// retire this handoff attempt and submit to the original conversation normally.
// Unknown outcomes and local persistence failures must not return this error.
var ErrConversationRenewalRejected = errors.New("conversation renewal rejected")

// ErrConversationRenewalUnknown retains the original attempted native create.
// The old bound context remains usable; never replay a create without its ID.
var ErrConversationRenewalUnknown = errors.New("conversation renewal outcome unknown")

// Deferred keeps the source binding and the original unused creation receipt.
var ErrConversationRenewalDeferred = errors.New("conversation renewal deferred")

type renewalGuardKey struct{}
type renewalCommitKey struct{}

// Checks may read required memory outside native locks. Commit performs the
// final human-priority check and adoption in one short host critical section.
func WithRenewalGuard(ctx context.Context, check func() error) context.Context {
	return context.WithValue(ctx, renewalGuardKey{}, check)
}

func CheckRenewalGuard(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if check, ok := ctx.Value(renewalGuardKey{}).(func() error); ok {
		return check()
	}
	return nil
}

func WithRenewalCommit(ctx context.Context, commit func(string, func() error) error) context.Context {
	return context.WithValue(ctx, renewalCommitKey{}, commit)
}

// The adapter keeps input admission closed until this returns. adopt only
// persists/publishes the prepared binding; native creation and reads precede it.
func CommitRenewal(ctx context.Context, session string, adopt func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if commit, ok := ctx.Value(renewalCommitKey{}).(func(string, func() error) error); ok {
		return commit(session, adopt)
	}
	return adopt()
}

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
	// TurnKey is the presentation identity of Turn, for attributing host events.
	TurnKey string
	// Host-only assembly versions; never user-facing Session navigation.
	RuntimeVersion, DesiredRuntimeVersion string
	// Observed is true only after native state has been restored. It is independent
	// of Idle: an observed running turn must still invalidate an older handoff.
	Observed bool
	Idle     bool
	// Usage is a live resident-context gauge, never cumulative billed tokens.
	// It is deliberately not persisted or restored from transcript history.
	Usage ContextUsage
	// UsageEvidence is an adapter-generated enum for anonymous diagnostics.
	UsageEvidence string
}

type ContextUsage struct {
	Used, Window int64
	// ModelAt is the last confirmed model response, not a tool/UI update.
	ModelAt time.Time
	// The latest resident model response's billing counters, when supplied.
	// These are observations for diagnostics, not an estimated session cost.
	InputTokens, OutputTokens, CacheReadTokens, CacheWriteTokens int64
	CacheWrite5mTokens, CacheWrite1hTokens                       int64
	// Cache TTL counters are parts of cache writes, never additional input.
	// Presence distinguishes an explicit zero from an unreported counter.
	CacheReadTokensReported, CacheWriteTokensReported      bool
	CacheWrite5mTokensReported, CacheWrite1hTokensReported bool
	LastProviderCost                                       float64
	LastProviderCostKnown                                  bool
	LastProviderCostCurrency                               string
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
