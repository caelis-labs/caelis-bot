package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const (
	defaultTypingPoll  = 250 * time.Millisecond
	defaultTypingRenew = 4 * time.Second
)

func typingForMainTurn(snapshot api.Snapshot) bool {
	return ready(snapshot) && snapshot.CurrentTurn != "" &&
		(snapshot.Phase == "sending" || snapshot.Phase == "working") &&
		len(snapshot.Approvals) == 0 && !snapshot.LoginPending
}

// typing owns one disposable loop per Telegram connection. Chat actions are
// presentation only: they never enter history, receipts, or Runtime recovery.
func (b *Bridge) typing(ctx context.Context, c client) {
	poll, renew := b.typingPoll, b.typingRenew
	if poll <= 0 {
		poll = defaultTypingPoll
	}
	if renew <= 0 {
		renew = defaultTypingRenew
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	var chat int64
	var active bool
	var next time.Time
	var rateLimitUntil time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		b.mu.Lock()
		currentChat, enabled := b.state.ChatID, b.state.Enabled && !b.closed &&
			b.issue != "network" && b.issue != "occupied" && b.issue != "invalid_token" && b.issue != "blocked"
		b.mu.Unlock()
		if currentChat != chat {
			chat, active, next = currentChat, false, time.Time{}
		}
		working := enabled && chat != 0 && typingForMainTurn(b.host.Snapshot())
		if !working {
			active = false
		} else if !time.Now().Before(rateLimitUntil) && (!active || !time.Now().Before(next)) {
			active = true
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.ChatAction(callCtx, chat)
			cancel()
			next = time.Now().Add(renew)
			var transport *transportError
			if errors.As(err, &transport) && transport.code == 429 {
				retry := time.Duration(max(transport.retry, 1)) * time.Second
				rateLimitUntil = time.Now().Add(retry)
				if retry > renew {
					next = rateLimitUntil
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
