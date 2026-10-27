package weixin

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// typing is disposable presentation state. It never affects ingress, sends, or
// the durable message ledger.
func (b *Bridge) typing(ctx context.Context, p *protocol) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var ticket string
	var active bool
	var nextSend, nextConfig time.Time
	var owner string
	defer func() {
		if active && ticket != "" {
			work, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_ = p.sendTyping(work, owner, ticket, false)
			stop()
		}
	}()
	for ctx.Err() == nil {
		snapshot := b.host.Snapshot()
		b.mu.Lock()
		owner, contextToken := b.state.OwnerID, b.state.ContextToken
		enabled := b.state.Enabled && b.issue != "auth_expired" && b.issue != "storage" && b.state.PauseUntil <= time.Now().UnixMilli()
		input, _ := latestUser(snapshot.Items)
		fromWeixin := input.RequestID != "" && input.RequestID == b.state.Window.InputID && b.state.Window.OwnerID == owner
		b.mu.Unlock()
		recovery := b.host.Recovery()
		working := enabled && owner != "" && fromWeixin && weixinTypingTurn(snapshot) &&
			!recovery.Automatic && !recovery.InProgress && !recovery.Manual
		if !working && active {
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			_ = p.sendTyping(work, owner, ticket, false)
			stop()
			active = false
		}
		if working && !time.Now().Before(nextConfig) {
			nextConfig = time.Now().Add(30 * time.Second)
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			found, err := p.getConfig(work, owner, contextToken)
			stop()
			if err == nil && found != "" {
				ticket = found
				nextConfig = time.Now().Add(24 * time.Hour)
			}
		}
		if working && ticket != "" && !time.Now().Before(nextSend) {
			started := time.Now()
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			err := p.sendTyping(work, owner, ticket, true)
			stop()
			active = err == nil
			nextSend = started.Add(5 * time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func weixinTypingTurn(snapshot api.Snapshot) bool {
	if snapshot.Connection != "ready" && snapshot.Connection != "connected" {
		return false
	}
	if snapshot.LoginPending || snapshot.CurrentTurn == "" || (snapshot.Phase != "sending" && snapshot.Phase != "working") {
		return false
	}
	for _, approval := range snapshot.Approvals {
		if approval.Status != "resolved" && approval.Owner != "task" {
			return false
		}
	}
	return true
}
