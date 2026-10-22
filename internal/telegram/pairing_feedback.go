package telegram

import (
	"context"
	"errors"
	"time"
)

const (
	pairingFeedbackInterval     = time.Second
	pairingFeedbackChatInterval = 30 * time.Second
	pairingFeedbackTimeout      = time.Second
)

// Unpaired senders have no output authority. Keep only a fixed number of recent
// reply slots in memory, independent of how many strangers contact the Bot.
// A full table refuses new replies until a slot expires; live slots are not evicted.
type pairingFeedbackBudget struct {
	next  time.Time
	chats [32]struct {
		id    int64
		until time.Time
	}
}

// allow runs under Bridge.mu. Admission spends both budgets before the send,
// including failed/uncertain sends. Suppressed updates never acquire a slot.
func (p *pairingFeedbackBudget) allow(chat int64, now time.Time) bool {
	if now.Before(p.next) {
		return false
	}
	free := -1
	for i, slot := range p.chats {
		if now.Before(slot.until) {
			if slot.id == chat {
				return false
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return false
	}
	p.chats[free].id, p.chats[free].until = chat, now.Add(pairingFeedbackChatInterval)
	p.next = now.Add(pairingFeedbackInterval)
	return true
}

// Candidate validation/binding has already finished. This best-effort hint must
// neither gate confirmation nor monopolize the serial control ingress lane.
// Consume it on timeout/error; never queue or automatically replay an uncertain send.
func (b *Bridge) sendPairingFeedback(ctx context.Context, c client, chat int64, body string) {
	b.mu.Lock()
	now := time.Now()
	allowed := ctx.Err() == nil && !now.Before(b.retryUntil) && b.pairingFeedback.allow(chat, now)
	b.mu.Unlock()
	if !allowed {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, pairingFeedbackTimeout)
	defer cancel()
	_, err := c.Send(ctx, chat, plainText(body), nil)
	b.mu.Lock()
	if b.outputRateLimitedLocked(err) {
		b.issue = "rate_limited"
	}
	b.mu.Unlock()
}

// All message/file output shares Telegram's retry_after cooldown. Concurrent
// rejections can extend it, but cannot shorten a longer outstanding cooldown.
func (b *Bridge) outputRateLimitedLocked(err error) bool {
	var transport *transportError
	if !errors.As(err, &transport) || transport.code != 429 {
		return false
	}
	until := time.Now().Add(time.Duration(max(transport.retry, 1)) * time.Second)
	if until.After(b.retryUntil) {
		b.retryUntil = until
	}
	return true
}
