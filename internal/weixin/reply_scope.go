package weixin

import "context"

// A send admission is bound to the paired account, recipient and one local
// window. It makes no claim about the output's causal Turn or input channel.
type replyScope struct{ inputID, ownerID, botID, deliveryKey string }
type replyScopeKey struct{}

func scheduled(ctx context.Context) bool {
	scope, ok := ctx.Value(replyScopeKey{}).(replyScope)
	return ok && scope.deliveryKey != ""
}
func (b *Bridge) scopeCurrent(ctx context.Context) bool {
	scope, ok := ctx.Value(replyScopeKey{}).(replyScope)
	if !ok || scope.deliveryKey == "" {
		return true
	}
	b.mu.Lock()
	var candidate queuedDelivery
	found := false
	for _, q := range b.state.Delivery.Queue {
		if q.Key == scope.deliveryKey {
			candidate = q
			found = true
			break
		}
	}
	valid := b.scopeWindowLocked(ctx)
	b.mu.Unlock()
	if !valid || !found || b.host.Snapshot == nil {
		return false
	}
	snapshot := b.host.Snapshot()
	// A newly surfaced P0/P1 must participate in this admission, not merely
	// validate the older low-priority candidate against the fresh snapshot.
	b.observeInteractions(snapshot)
	return b.deliveryActionCurrent(candidate, snapshot)
}
func (b *Bridge) scopeWindowLocked(ctx context.Context) bool {
	scope, ok := ctx.Value(replyScopeKey{}).(replyScope)
	if !ok {
		return true
	}
	if b.closed || b.updating {
		return false
	}
	if scope.inputID != b.state.Window.InputID {
		return false
	}
	if scope.deliveryKey == "" {
		return true
	}
	if scope.ownerID != b.state.OwnerID || scope.botID != b.state.BotID {
		return false
	}
	index := b.nextDeliveryLocked()
	return index >= 0 && b.state.Delivery.Queue[index].Key == scope.deliveryKey
}

func (b *Bridge) commandContext(ctx context.Context, id string) context.Context {
	// Local feedback can precede the asynchronous chat publication. It is bound
	// to its original authenticated input, and can never use a newer allowance.
	return context.WithValue(ctx, replyScopeKey{}, replyScope{inputID: id})
}
