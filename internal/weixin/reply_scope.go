package weixin

import (
	"context"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// A delivery scope is a projection of the shared conversation and authenticated
// ingress, not a second conversation/turn owner. Native adapters classify host
// reports and scheduled inputs before publishing Kind=user. Never inspect prose.
type replyScope struct{ inputID, itemID string }
type replyScopeKey struct{}

func latestUser(items []api.Item) (api.Item, int) {
	index := -1
	for i, item := range items {
		if item.Kind != "user" {
			continue
		}
		// The IM keeps the original SeenAt when a native echo is restored. An old
		// echo appended after paging must not supersede a newer real input.
		if index < 0 || item.SeenAt == 0 || items[index].SeenAt == 0 || item.SeenAt >= items[index].SeenAt {
			index = i
		}
	}
	if index < 0 {
		return api.Item{}, -1
	}
	return items[index], index
}

func replyTail(snapshot api.Snapshot) (api.Item, []api.Item) {
	input, at := latestUser(snapshot.Items)
	if at < 0 {
		return input, nil
	} // incomplete/restoring history cannot authorize catch-up
	items := []api.Item{input}
	for i, item := range snapshot.Items {
		if i <= at || input.SeenAt != 0 && item.SeenAt != 0 && item.SeenAt <= input.SeenAt {
			continue
		}
		items = append(items, item)
	}
	return input, items
}

func (b *Bridge) scopeReplies(ctx context.Context, snapshot api.Snapshot) (context.Context, api.Snapshot, bool) {
	input, tail := replyTail(snapshot)
	b.mu.Lock()
	defer b.mu.Unlock()
	// Window.InputID comes only from a distinct authenticated paired-owner
	// getupdates message, not a later submit receipt or the current native Turn.
	active := input.RequestID != "" && input.RequestID == b.state.Window.InputID && b.state.Window.OwnerID == b.state.OwnerID
	eligible := map[string]bool{}
	if active {
		for _, item := range tail {
			eligible[item.ID] = true
		}
	}
	// Unknown history while reconnecting waits. Known changed boundaries expire
	// old queues without spending a send slot or altering attempted receipts.
	if input.ID == "" {
		for _, pending := range b.state.Progress {
			if pending.InputID == b.state.Window.InputID {
				eligible[pending.ID] = true
			}
		}
	}
	if !b.expireRepliesLocked(eligible, active || input.ID == "", input.ID != "") {
		return ctx, snapshot, false
	}
	if !active {
		return ctx, snapshot, false
	}
	snapshot.Items = tail
	return context.WithValue(ctx, replyScopeKey{}, replyScope{input.RequestID, input.ID}), snapshot, true
}

func (b *Bridge) expireRepliesLocked(eligible map[string]bool, active, known bool) bool {
	changed := false
	keep := b.state.Progress[:0]
	for _, pending := range b.state.Progress {
		if active && pending.InputID == b.state.Window.InputID && eligible[pending.ID] {
			keep = append(keep, pending)
			continue
		}
		if old := b.state.Outputs["item:"+pending.ID]; old.State == "" || old.State == "skip" {
			b.state.Outputs["item:"+pending.ID] = outbound{State: "superseded"}
		}
		changed = true
	}
	b.state.Progress = keep
	if changed {
		b.state.ProgressBatch = nil
		b.state.FinalProgressFor = ""
		b.state.FinalProgressIDs = nil
	}
	if id := b.state.PendingFinal; id != "" && known && (!active || !eligible[id]) {
		b.state.Outputs["item:"+id] = outbound{State: "superseded"}
		b.state.PendingFinal = ""
		changed = true
	}
	for key, out := range b.state.Outputs {
		if (out.State == "pending" || out.State == "preparing") && (!active || out.FileInputID != b.state.Window.InputID) {
			out.State, out.Reason = "superseded", "reply_boundary_changed"
			b.state.Outputs[key] = out
			changed = true
		}
	}
	if !changed {
		return true
	}
	// On failure the in-memory fence remains closed; storage failure also blocks
	// output. Reload still re-derives the same fence from the shared conversation.
	return b.saveLocked() == nil
}

// Recheck between chunks and after asynchronous file preparation. Once an intent
// has been durably reserved it is in flight; its actual result is never rewritten.
func (b *Bridge) scopeCurrent(ctx context.Context) bool {
	scope, scoped := ctx.Value(replyScopeKey{}).(replyScope)
	if !scoped {
		return true
	} // low-level transport; production callers scope admission
	if scope.itemID == "" {
		return true
	} // direct local feedback is ingress-bound
	if b.host.Snapshot == nil {
		return false
	}
	input, _ := latestUser(b.host.Snapshot().Items)
	return input.RequestID == scope.inputID && input.ID == scope.itemID
}

func (b *Bridge) scopeWindowLocked(ctx context.Context) bool {
	scope, scoped := ctx.Value(replyScopeKey{}).(replyScope)
	return !scoped || scope.inputID == b.state.Window.InputID
}

func (b *Bridge) commandContext(ctx context.Context, id string) context.Context {
	// Local feedback can precede the asynchronous chat publication. It is bound
	// to its original authenticated input, and can never use a newer allowance.
	return context.WithValue(ctx, replyScopeKey{}, replyScope{inputID: id})
}

// Keep native source validation for background reports and tasks. A real new
// input in the same native Turn supersedes older steering for reply routing.
func (b *Bridge) ownsReplyTurn(snapshot api.Snapshot, turn string) bool {
	input, _ := latestUser(snapshot.Items)
	if input.TurnKey == turn && input.RequestID != "" {
		b.mu.Lock()
		accepted := b.state.AcceptedInputID == input.RequestID || b.state.Inputs[input.RequestID] == "accepted"
		b.mu.Unlock()
		if accepted && snapshot.RuntimeOwner != "" {
			for _, source := range snapshot.InputSources {
				if source.RequestID == input.RequestID && source.TurnKey == turn && source.RuntimeOwner == snapshot.RuntimeOwner {
					return true
				}
			}
		}
	}
	return b.ownsTurn(snapshot, turn)
}
