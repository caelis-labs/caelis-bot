package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) callDreamTool(ctx context.Context, args map[string]any) api.ToolResult {
	r.continuity.Lock()
	generation := r.userGeneration
	r.continuity.Unlock()
	invocation, ok := api.ToolInvocationFromContext(ctx)
	if !ok || invocation.Provider != r.provider || len(invocation.CallID) > 256 {
		return compactError("invocation_unavailable", errors.New("original resident tool call is unavailable"))
	}
	if r.handoff == nil {
		return compactError("handoff_unavailable", errors.New("private Bot handoff store is unavailable"))
	}
	text := strings.TrimSpace(str(args, "handoff"))
	if text == "" || len(text) > 16<<10 {
		return compactError("invalid_handoff", errors.New("handoff must be a complete nonempty summary of at most 16 KiB"))
	}
	memory, err := r.dreamMemoryDigest(ctx)
	if err != nil {
		return compactError("memory_unavailable", err)
	}
	r.continuity.Lock()
	defer r.continuity.Unlock()
	if generation != r.userGeneration || r.registeringUser != 0 || r.lastHumanTurn.Session == invocation.Session && r.lastHumanTurn.Turn == invocation.Turn {
		return compactError("handoff_superseded", errors.New("User activity superseded this Dream; continue in the current conversation"))
	}
	if err := r.handoff.saveTurnWithMemory(invocation.CallID, invocation.Session, invocation.Turn, text, memory); err != nil {
		return compactError("handoff_unavailable", err)
	}
	out := compactValue(map[string]any{"ok": true, "data": map[string]any{"callId": invocation.CallID, "sourceSession": invocation.Session, "turn": invocation.Turn, "status": "accepted"}}, false)
	out.TurnComplete = true
	out.CheckTurnComplete = func() error { return r.checkDreamCompletion(invocation) }
	return out
}

func (r *Runtime) completeCodexDream(ctx context.Context, invocation api.ToolInvocation) error {
	terminator, ok := r.engine.(api.ToolTurnTerminator)
	if !ok {
		return errors.New("native turn termination is unavailable")
	}
	ctx = api.WithRenewalGuard(ctx, func() error { return r.checkDreamCompletion(invocation) })
	if err := terminator.CompleteToolTurn(ctx, invocation); err != nil {
		return err
	}
	return r.renewToolHandoff(ctx)
}

func (r *Runtime) renewToolHandoff(ctx context.Context) error {
	r.handoffRenewal.Lock()
	defer r.handoffRenewal.Unlock()
	if r.handoff == nil {
		return nil
	}
	pending := r.handoff.pending()
	if pending.CallID == "" || pending.NewSession != "" || pending.Turn == "" {
		return nil
	}
	renewer, ok := r.engine.(api.ToolContextRenewer)
	if !ok {
		return errors.New("native context renewal is unavailable")
	}
	session, err := renewer.RenewAfterTool(r.guardRenewal(ctx, pending), api.ToolInvocation{Provider: r.provider, CallID: pending.CallID, Session: pending.Source, Turn: pending.Turn})
	if err != nil {
		if errors.Is(err, api.ErrConversationRenewalDeferred) {
			return r.handoff.record(pending.CallID, "", "fallback")
		}
		if errors.Is(err, api.ErrConversationRenewalRejected) {
			return r.handoff.record(pending.CallID, "", "rejected")
		}
		if errors.Is(err, api.ErrConversationRenewalUnknown) {
			// The adapter has retained the original unknown create receipt.
			// Archive its summary without fencing unrelated old-session tools.
			if saveErr := r.handoff.record(pending.CallID, "", "unknown_fallback"); saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	if current := r.handoff.pending(); current.CallID == pending.CallID && current.NewSession == "" {
		// Recover a native binding committed before a failed host receipt write.
		return r.handoff.record(pending.CallID, session, "committed")
	}
	return nil
}

// RenewPendingToolHandoff runs after the resident callback Turn becomes
// terminal. The original callback receipt remains the only create authority.
func (r *Runtime) RenewPendingToolHandoff(ctx context.Context) error {
	return r.renewToolHandoff(ctx)
}
