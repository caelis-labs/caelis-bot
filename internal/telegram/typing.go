package telegram

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

const (
	defaultTypingPoll  = 250 * time.Millisecond
	defaultTypingRenew = 4 * time.Second
)

// TypingForMainTurn evaluates the production gate for a resident turn.
// Adapters, not turn-key comparisons in the transport, own approval scope.
func TypingForMainTurn(snapshot api.Snapshot) bool {
	return typingMainState(snapshot) == "active"
}

func typingMainState(snapshot api.Snapshot) string {
	switch {
	case !ready(snapshot):
		return "runtime_unavailable"
	case snapshot.LoginPending:
		return "login_pending"
	case snapshot.CurrentTurn == "":
		return "no_main_turn"
	case snapshot.Phase != "sending" && snapshot.Phase != "working":
		return "phase_inactive"
	}
	for _, approval := range snapshot.Approvals {
		// Resolved cards remain in native history after the main turn resumes.
		// Only a backend-confirmed independent task may coexist with typing.
		// Missing scope and main-conversation children still pause conservatively.
		if approval.Status != "resolved" && approval.Owner != "task" {
			return "approval_unresolved"
		}
	}
	return "active"
}

func typingActionResult(err error, timedOut bool) (string, int) {
	// The SDK classifies an expired request as a network transport error, so
	// preserve the timeout fact from our own call context before cancellation.
	if timedOut {
		return "timeout", 0
	}
	if err == nil {
		return "ok", 0
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", 0
	}
	var transport *transportError
	if errors.As(err, &transport) {
		if transport.code == 429 {
			return "rate_limited", 429
		}
		if transport.code != 0 {
			return "http_error", transport.code
		}
	}
	return "transport_error", 0
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
	var lastState, lastResult string
	var lastResultCode int
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
		state := "bridge_inactive"
		if enabled && chat != 0 {
			recovery := b.recoveryState()
			if recovery.Automatic || recovery.InProgress {
				state = "recovery"
			} else {
				state = typingMainState(b.host.Snapshot())
			}
		}
		if state != lastState {
			b.typingDiagnostic(diagnosticlog.Record{Level: "info", Component: "telegram", Code: "typing_state", Phase: state})
			lastState, lastResult, lastResultCode = state, "", 0
		}
		working := state == "active"
		if !working {
			active = false
		} else if !time.Now().Before(rateLimitUntil) && (!active || !time.Now().Before(next)) {
			active = true
			started := time.Now()
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := c.ChatAction(callCtx, chat)
			timedOut := errors.Is(callCtx.Err(), context.DeadlineExceeded)
			cancel()
			if ctx.Err() != nil {
				return
			}
			// Count call time inside the renewal interval. A slow call must not
			// turn a four-second cadence into a six-second typing gap.
			next = started.Add(renew)
			result, code := typingActionResult(err, timedOut)
			if result != lastResult || code != lastResultCode {
				level := "info"
				if err != nil {
					level = "warning"
				}
				b.typingDiagnostic(diagnosticlog.Record{Level: level, Component: "telegram", Code: "typing_action_result", Phase: result, HTTPStatus: code, ProcessingMS: time.Since(started).Milliseconds()})
				lastResult, lastResultCode = result, code
			}
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

func (b *Bridge) typingDiagnostic(record diagnosticlog.Record) {
	if b.host.Diagnostics != nil {
		b.host.Diagnostics(record)
	}
}
