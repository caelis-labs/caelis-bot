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
	windowSendLimit = 10
	windowSoftLimit = 8
	windowMaxAge    = 12 * time.Hour
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
	if !fitsText(body) || body == "" {
		return "", "unavailable"
	}
	b.mu.Lock()
	if old, exists := b.state.Outputs[key]; exists {
		b.mu.Unlock()
		if old.State == "accepted" && old.Digest == textDigest(body) {
			return old.MessageID, old.State
		}
		return "", old.State
	}
	if b.remainingLocked(critical) == 0 {
		b.mu.Unlock()
		return "", "deferred"
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		b.mu.Unlock()
		return "", "unavailable"
	}
	oldWindow := b.state.Window
	b.state.Window.Used++
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
