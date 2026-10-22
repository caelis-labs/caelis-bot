package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	tg "github.com/mymmrac/telego"
)

// Synthetic direct and durable-ingress coverage of the unauthenticated reply boundary.
func TestUnpairedStartHonors429Cooldown(t *testing.T) {
	for _, text := range []string{"/start", "/start SYNTHETIC_review"} {
		t.Run(text, func(t *testing.T) {
			b, f := testBridge(t, Host{})
			b.state.Enabled, b.state.Bot = true, "fixture_caelis_bot"
			b.nonce, b.expires = "SYNTHETIC_review", time.Now().Add(time.Minute)
			f.sendErr = &transportError{issue: "rate_limited", code: 429, retry: 30}
			for i := 1; i <= 12; i++ {
				if !b.input(t.Context(), f, message(i, 10, 20, text)) {
					t.Fatal("update was not consumed")
				}
			}
			if f.sends != 1 {
				t.Fatalf("ordinary unauthenticated /start attempted %d sends despite the first send returning HTTP 429 retry_after=30; expected cooldown", f.sends)
			}
			if text != "/start" && (b.Status().Candidate == "" || b.Status().Paired) {
				t.Fatal("acknowledgement failure changed candidate authorization")
			}
		})
	}
}

func TestPairingFeedbackBudgetsBoundBurstsAndRecover(t *testing.T) {
	var p pairingFeedbackBudget
	now := time.Now()
	if !p.allow(10, now) {
		t.Fatal("first hint refused")
	}
	for i := range 1000 {
		if p.allow(int64(i+1), now) {
			t.Fatal("aggregate burst budget bypassed")
		}
	}
	if p.allow(10, now.Add(pairingFeedbackChatInterval-time.Nanosecond)) {
		t.Fatal("same chat bypassed cooldown")
	}
	if !p.allow(11, now.Add(pairingFeedbackInterval)) || !p.allow(10, now.Add(pairingFeedbackChatInterval)) {
		t.Fatal("aggregate or per-chat budget did not recover")
	}
	// Saturate with live slots without using an unbounded map or evicting a
	// stranger's cooldown. Slot expiry alone makes capacity available again.
	p = pairingFeedbackBudget{}
	for i := range p.chats {
		p.chats[i].id, p.chats[i].until = int64(i+1), now.Add(time.Minute)
	}
	if p.allow(99, now) || p.allow(1, now) || !p.allow(99, now.Add(time.Minute)) {
		t.Fatal("full table evicted a live slot or failed to recover")
	}
}

func TestStartBurstsConsumeUpdatesWithoutReplayingHints(t *testing.T) {
	b, f := testBridge(t, Host{})
	for i := range 1000 {
		if !b.input(t.Context(), f, message(i+1, int64(i+10), int64(i+20), "/start SYNTHETIC_wrong")) {
			t.Fatal("suppressed update retained for retry")
		}
	}
	if f.sends != 1 || len(b.state.Inputs) != 0 || len(b.state.Messages) != 0 || b.candidate != nil {
		t.Fatal("untrusted burst sent repeatedly, persisted output state or bound a candidate")
	}
	if strings.Contains(f.texts[0], "SYNTHETIC_") {
		t.Fatal("guidance disclosed a pairing payload")
	}
}

func TestPairingFeedbackSharesOutputCooldownAndRecoversWithoutReplay(t *testing.T) {
	for _, source := range []string{"pairing", "normal-output"} {
		t.Run(source, func(t *testing.T) {
			b, f := testBridge(t, Host{})
			f.sendErr = &transportError{issue: "rate_limited", code: 429, retry: 30}
			if source == "pairing" {
				b.input(t.Context(), f, message(1, 10, 20, "/start"))
			} else {
				b.sendText(t.Context(), f, "normal", 10, "ordinary output", nil)
			}
			b.mu.Lock()
			until := b.retryUntil
			b.mu.Unlock()
			if time.Until(until) < 29*time.Second {
				t.Fatal("server retry_after was not retained")
			}
			b.input(t.Context(), f, message(2, 99, 88, "/start"))
			b.sendText(t.Context(), f, "later", 10, "later output", nil)
			if f.sends != 1 {
				t.Fatal("pairing and ordinary output did not share cooldown")
			}
			b.mu.Lock()
			// A concurrent shorter rejection must not shorten server cooldown.
			b.outputRateLimitedLocked(&transportError{code: 429, retry: 1})
			if b.retryUntil.Before(until) {
				t.Fatal("shorter rejection shortened cooldown")
			}
			b.retryUntil = time.Now().Add(-time.Second)
			// Advance the aggregate admission clock as well, as it expires
			// before the 30-second server cooldown in production.
			b.pairingFeedback.next = time.Now().Add(-time.Second)
			b.mu.Unlock()
			f.sendErr = nil
			// Only a new update produces a new hint; suppressed updates never
			// enter an outbox to be replayed when the cooldown expires.
			b.input(t.Context(), f, message(3, 99, 88, "/start"))
			if f.sends != 2 {
				t.Fatal("fresh update could not send after cooldown")
			}
		})
	}
}

func TestValidPairCandidateSurvivesFeedbackThrottle(t *testing.T) {
	for _, throttle := range []string{"per-chat", "aggregate", "server"} {
		t.Run(throttle, func(t *testing.T) {
			b, f := testBridge(t, Host{})
			b.state.Enabled, b.state.Bot = true, "fixture_caelis_bot"
			b.nonce, b.expires = "SYNTHETIC_current", time.Now().Add(time.Minute)
			switch throttle {
			case "per-chat":
				b.input(t.Context(), f, message(1, 10, 20, "/start"))
				b.pairingFeedback.next = time.Time{}
			case "aggregate":
				b.input(t.Context(), f, message(1, 99, 88, "/start"))
			case "server":
				b.retryUntil = time.Now().Add(time.Minute)
			}
			before := f.sends
			b.input(t.Context(), f, message(2, 10, 20, "/start "+b.nonce))
			if f.sends != before || b.Status().Candidate == "" || b.Status().Paired {
				t.Fatal("throttled feedback prevented candidate binding or auto-authorized it")
			}
			b.input(t.Context(), f, message(3, 99, 88, "/start "+b.nonce))
			if b.candidate.From.ID != 20 {
				t.Fatal("throttle allowed another account to replace candidate")
			}
			if s, err := b.Confirm(); err != nil || !s.Paired || s.PairURL != "" {
				t.Fatal("throttled candidate could not be explicitly confirmed")
			}
		})
	}
}

type slowPairingFeedbackClient struct{ *fakeClient }

func (f slowPairingFeedbackClient) Send(ctx context.Context, chat int64, body outgoingText, keys *tg.InlineKeyboardMarkup) (int, error) {
	_, _ = f.fakeClient.Send(ctx, chat, body, keys)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestPollingSlowStartFeedbackCannotHoldCandidateBehindBurst(t *testing.T) {
	b, f := testBridge(t, Host{})
	b.newClient = func(string) (client, error) { return slowPairingFeedbackClient{f}, nil }
	if _, err := b.Connect(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	nonce := b.nonce
	b.mu.Unlock()
	updates := make([]tg.Update, 12)
	for i := range updates {
		updates[i] = message(i+1, 10, 20, "/start")
	}
	updates = append(updates, message(13, 10, 20, "/start "+nonce))
	f.updates <- updates
	deadline := time.Now().Add(4 * time.Second)
	for {
		b.mu.Lock()
		done := b.state.Offset == 14 && len(b.state.Ingress) == 0
		b.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow hint held control ingress or valid candidate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	sends := f.sends
	f.mu.Unlock()
	if sends != 1 || b.Status().Candidate == "" || b.Status().Paired {
		t.Fatal("timeout retried uncertain send or lost pending candidate")
	}
	if s, err := b.Confirm(); err != nil || !s.Paired {
		t.Fatal("slow-feedback candidate could not be explicitly confirmed")
	}
}

func TestPollingUnpairedStartHonors429Cooldown(t *testing.T) {
	b, f := testBridge(t, Host{})
	f.sendErr = &transportError{issue: "rate_limited", code: 429, retry: 30}
	if _, err := b.Connect(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	nonce := b.nonce
	b.mu.Unlock()
	updates := make([]tg.Update, 12)
	for i := range updates {
		updates[i] = message(i+1, int64(i+100), int64(i+200), "/start")
	}
	updates = append(updates, message(13, 10, 20, "/start "+nonce))
	f.updates <- updates
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		done := b.state.Offset == 14 && len(b.state.Ingress) == 0
		b.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("poller did not drain the synthetic updates")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	sends := f.sends
	f.mu.Unlock()
	if sends != 1 {
		t.Fatalf("actual poller/ingress attempted %d sends during a 30-second 429 cooldown", sends)
	}
	if b.Status().Candidate == "" || b.Status().Paired {
		t.Fatal("429 burst lost the valid candidate or authorized it")
	}
	if s, err := b.Confirm(); err != nil || !s.Paired {
		t.Fatal("candidate received during cooldown could not be explicitly confirmed")
	}
}
