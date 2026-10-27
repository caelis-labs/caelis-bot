package weixin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// These are local conservative estimates, not a Tencent service contract.
// Every attempted sendmessage consumes a slot, including a rejected or unknown
// outcome. Presence and typing do not create a new message window.
const (
	windowSendLimit   = 10
	windowSoftLimit   = windowSendLimit - 1 // keep one attempt for a terminal reply
	progressSendLimit = 5
	windowMaxAge      = 12 * time.Hour
)

func (b *Bridge) remainingLocked(critical bool) int {
	w := b.state.Window
	if w.OwnerID == "" || w.OwnerID != b.state.OwnerID || w.InputID == "" || w.ContextToken == "" || w.InputAt == 0 || w.Exhausted || time.Since(time.UnixMilli(w.InputAt)) > windowMaxAge {
		return 0
	}
	limit := windowSoftLimit
	if critical {
		limit = windowSendLimit
	}
	if w.Used >= limit {
		return 0
	}
	return limit - w.Used
}

func (b *Bridge) remainingProgressLocked() int {
	remaining := b.remainingLocked(false)
	if quota := progressSendLimit - b.state.Window.ProgressUsed; quota < remaining {
		return quota
	}
	return remaining
}

const exhaustedHint = "本轮微信推送预计已用尽（10/10）。请回复 Bot 指定还需要的内容；完整记录保留在桌面。"

func withExhaustedHint(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	last := len(lines) - 1
	// Keep a card's copyable instruction as its last line.
	if last >= 0 && (strings.HasPrefix(lines[last], "提示：") || strings.HasPrefix(lines[last], "**提示：")) {
		lines = append(lines[:last], append([]string{"", exhaustedHint, ""}, lines[last:]...)...)
		return strings.Join(lines, "\n")
	}
	return body + "\n\n" + exhaustedHint
}

func sendErrorClass(message string) string {
	s := strings.ToLower(strings.TrimSpace(message))
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "rate limit") || strings.Contains(s, "too many") || strings.Contains(s, "quota"):
		return "rate_limited"
	case strings.Contains(s, "context") && (strings.Contains(s, "expir") || strings.Contains(s, "invalid")):
		return "context_expired"
	case strings.Contains(s, "prepare fail"):
		return "prepare_failed"
	case strings.Contains(s, "auth") || strings.Contains(s, "token") && strings.Contains(s, "invalid"):
		return "authentication"
	default:
		return "other"
	}
}

// sendOne saves the exact intent and consumes its estimated slot before HTTP.
// A transport/response failure is permanently unknown: client_id is not a
// documented deduplication guarantee, so it cannot authorize another send.
func (b *Bridge) sendOne(ctx context.Context, p *protocol, key, body string, critical bool) (string, string) {
	return b.sendOnePolicy(ctx, p, key, body, critical, false)
}

func (b *Bridge) sendProgressOne(ctx context.Context, p *protocol, key, body string) (string, string) {
	return b.sendOnePolicy(ctx, p, key, body, false, true)
}

func (b *Bridge) sendOnePolicy(ctx context.Context, p *protocol, key, body string, critical, progress bool) (string, string) {
	if !fitsText(body) || body == "" {
		return "", "unavailable"
	}
	if ctx.Err() != nil || !b.scopeCurrent(ctx) {
		return "", "superseded"
	}
	b.mu.Lock()
	if !b.scopeWindowLocked(ctx) {
		b.mu.Unlock()
		return "", "superseded"
	}
	if old, exists := b.state.Outputs[key]; exists {
		b.mu.Unlock()
		if old.State == "accepted" && old.Digest == textDigest(body) {
			return old.MessageID, old.State
		}
		return "", old.State
	}
	remaining := b.remainingLocked(critical)
	if progress {
		remaining = b.remainingProgressLocked()
	}
	if remaining == 0 {
		b.mu.Unlock()
		return "", "deferred"
	}
	if b.state.Window.Used == windowSendLimit-1 {
		if decorated := withExhaustedHint(body); fitsText(decorated) {
			body = decorated
		}
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		b.mu.Unlock()
		return "", "unavailable"
	}
	oldWindow := b.state.Window
	b.state.Window.Used++
	if progress {
		b.state.Window.ProgressUsed++
	}
	w := b.state.Window
	intent := outbound{
		State: "unknown", ClientID: "caelis-weixin-" + hex.EncodeToString(seed[:]),
		ContextToken: w.ContextToken, Digest: textDigest(body), Attempts: 1,
		Bytes: len(body), Units: textUnits(body), InputAgeSec: int64(time.Since(time.UnixMilli(w.InputAt)).Seconds()), WindowUsed: w.Used,
	}
	b.state.Outputs[key] = intent
	if b.saveLocked() != nil {
		b.state.Window = oldWindow
		delete(b.state.Outputs, key)
		b.mu.Unlock()
		return "", "unavailable"
	}
	owner := b.state.OwnerID
	b.mu.Unlock()
	work, cancel := context.WithTimeout(ctx, 15*time.Second)
	result, err := p.send(work, owner, intent.ContextToken, intent.ClientID, body)
	cancel()
	b.mu.Lock()
	state, reason := "accepted", "ret_zero"
	if err != nil {
		state, reason = "unknown", "transport_or_response"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "timeout"
		} else if strings.HasPrefix(err.Error(), "http_") {
			reason = "http_error"
		}
		b.issue = "delivery_uncertain"
	} else {
		intent.Ret, intent.ErrCode = result.Ret, &result.ErrCode
		intent.ErrorClass = sendErrorClass(result.ErrMsg)
		if result.Ret == nil && result.ErrCode == 0 && intent.ErrorClass == "other" {
			state, reason = "unknown", "unclassified_response"
			b.issue = "delivery_uncertain"
		} else if (result.Ret != nil && *result.Ret != 0) || result.ErrCode != 0 || intent.ErrorClass != "" && intent.ErrorClass != "other" {
			state, reason = "rejected", "business_rejected"
			b.issue = "send_rejected"
			if result.Ret != nil && *result.Ret == -14 || result.ErrCode == -14 || intent.ErrorClass == "authentication" {
				b.issue = "auth_expired"
			}
			// Only an explicit rate-limit/context signal closes the local window.
			if b.state.Window.InputID == w.InputID && (intent.ErrorClass == "rate_limited" || intent.ErrorClass == "context_expired") {
				b.state.Window.Exhausted = true
			}
		} else {
			intent.MessageID = string(result.MessageID)
			if result.Ret == nil {
				reason = "http_success_no_ret"
			}
		}
	}
	intent.State, intent.Reason = state, reason
	b.state.Outputs[key] = intent
	_ = b.saveLocked()
	b.mu.Unlock()
	if state == "accepted" {
		return intent.MessageID, state
	}
	return "", state
}
