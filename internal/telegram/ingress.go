package telegram

import (
	"context"
	tg "github.com/mymmrac/telego"
	"strings"
	"time"
)

func ingressKind(u tg.Update) int {
	if u.CallbackQuery != nil {
		return 0
	}
	if u.Message != nil {
		switch strings.Split(u.Message.Text, " ")[0] {
		case "/status", "/stop", "/start":
			return 1
		}
	}
	return 2
}

// Receiving and checkpointing never waits for Runtime readiness, a download or
// inference. Callback, control and ordinary IM dispatch have separate lanes.
func (b *Bridge) processIngress(ctx context.Context, c client, kind int) {
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for ctx.Err() == nil {
		b.mu.Lock()
		var next *tg.Update
		for _, u := range b.state.Ingress {
			if ingressKind(u) == kind {
				value := u
				if raw := b.secretIngress[u.UpdateID]; raw != "" && value.Message != nil {
					copyMessage := *value.Message
					copyMessage.Text = raw
					value.Message = &copyMessage
				}
				next = &value
				break
			}
		}
		b.mu.Unlock()
		if next != nil {
			work, cancel := context.WithTimeout(ctx, 30*time.Second)
			if kind == 0 || kind == 1 {
				work = ctx
			}
			ok, deferred := b.inputResult(work, c, *next)
			cancel()

			if ok && !deferred {
				b.mu.Lock()
				delete(b.secretIngress, next.UpdateID)
				delete(b.state.SecretPrompts, next.UpdateID)
				for n, u := range b.state.Ingress {
					if u.UpdateID == next.UpdateID {
						b.state.Ingress = append(b.state.Ingress[:n], b.state.Ingress[n+1:]...)
						break
					}
				}
				_ = b.saveLocked()
				b.mu.Unlock()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (b *Bridge) answer(ctx context.Context, c client, id, text string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = c.Answer(ctx, id, text)
}
