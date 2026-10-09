package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) callDreamTool(ctx context.Context, args map[string]any) api.ToolResult {
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
	if err := r.handoff.saveTurn(invocation.CallID, invocation.Session, invocation.Turn, text); err != nil {
		return compactError("handoff_unavailable", err)
	}
	out := compactValue(map[string]any{"ok": true, "data": map[string]any{"callId": invocation.CallID, "sourceSession": invocation.Session, "turn": invocation.Turn, "status": "accepted"}}, false)
	out.TurnComplete = true
	return out
}

func (r *Runtime) completeCodexDream(ctx context.Context, invocation api.ToolInvocation) error {
	terminator, ok := r.engine.(api.ToolTurnTerminator)
	if !ok {
		return errors.New("native turn termination is unavailable")
	}
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
	session, err := renewer.RenewAfterTool(ctx, api.ToolInvocation{Provider: r.provider, CallID: pending.CallID, Session: pending.Source, Turn: pending.Turn})
	if err != nil {
		if errors.Is(err, api.ErrConversationRenewalRejected) {
			return r.handoff.record(pending.CallID, "", "rejected")
		}
		if errors.Is(err, api.ErrConversationRenewalUnknown) {
			if saveErr := r.handoff.record(pending.CallID, "", "unknown_fallback"); saveErr != nil {
				return saveErr
			}
		}
		return err
	}
	return r.handoff.record(pending.CallID, session, "committed")
}

// RenewPendingToolHandoff runs after the resident callback Turn becomes
// terminal. The original callback receipt remains the only create authority.
func (r *Runtime) RenewPendingToolHandoff(ctx context.Context) error {
	return r.renewToolHandoff(ctx)
}
