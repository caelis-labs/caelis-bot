package codex

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

const maxAutoReconnectAttempts = 5

func resourceExhausted(err error) bool {
	return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOMEM) || errors.Is(err, syscall.EAGAIN)
}

func recoverableDisconnect(err error) bool {
	if resourceExhausted(err) {
		return false
	}
	return errors.Is(err, ErrClosed) || errors.Is(err, ErrWebSocketClose) ||
		errors.Is(err, ErrWebSocketReset) || errors.Is(err, ErrIO) || errors.Is(err, ErrEventOverflow) ||
		errors.Is(err, ErrJSONDecode) || errors.Is(err, ErrFrameTooLarge) || errors.Is(err, ErrProtocol)
}

func (s *Session) retryDelay(attempt int) time.Duration {
	if s.reconnectDelay != nil {
		return s.reconnectDelay(attempt)
	}
	return (250 * time.Millisecond) << min(attempt-1, 4)
}

// Called under s.mu after the original listener has stopped. The one coordinator
// owns all retries; listeners created by its attempts cannot start a second one.
func (s *Session) scheduleAutoReconnectLocked(c *Client, epoch uint64) {
	if s.reconnectActive || s.closed || s.closing || (!s.bound && !s.workerOnly) || !recoverableDisconnect(c.Err()) {
		return
	}
	ctx, cancel := context.WithCancel(s.life)
	s.reconnectSeq++
	seq := s.reconnectSeq
	s.reconnectActive = true
	s.reconnectCancel = cancel
	s.reconnectAttempts = 0
	s.state.Message = "连接中断，正在核对原任务和发送结果。"
	s.update()
	go s.autoReconnect(ctx, seq, epoch)
}

func (s *Session) autoReconnect(ctx context.Context, seq, epoch uint64) {
	defer func() {
		s.mu.Lock()
		if s.reconnectSeq == seq {
			s.reconnectActive = false
			s.reconnectCancel = nil
			s.update()
		}
		s.mu.Unlock()
	}()
	for attempt := 1; attempt <= maxAutoReconnectAttempts; attempt++ {
		timer := time.NewTimer(s.retryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.op.Lock()
		s.mu.Lock()
		current := s.reconnectSeq == seq && s.epoch == epoch && !s.closed && !s.closing && ctx.Err() == nil
		if current {
			s.reconnectAttempts = attempt
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "reconnect_attempt", Generation: epoch, SessionEpoch: epoch, Phase: "original_owner", Limit: maxAutoReconnectAttempts, Sequence: uint64(attempt)})
		}
		s.mu.Unlock()
		if !current {
			s.op.Unlock()
			return
		}
		err := s.connect(ctx)
		s.op.Unlock()
		s.mu.Lock()
		if s.reconnectSeq != seq || s.closed || s.closing {
			s.mu.Unlock()
			return
		}
		epoch = s.epoch
		ready := err == nil && s.state.Connection == "ready"
		// A connected owner can have an unresolved exact cleanup receipt. Retrying
		// Connect here could repeat cleanup whose prior effect is unknown.
		needsReview := err != nil && s.state.Connection == "ready"
		stop := err == nil || needsReview || incompatibleProtocol(s.lastConnectCause) || resourceExhausted(s.lastConnectCause) || errors.Is(s.lastConnectCause, errRuntimeMissing)
		if ready {
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "reconnect_ready", Generation: epoch, SessionEpoch: epoch, Phase: "original_receipts", Sequence: uint64(attempt)})
		}
		if needsReview {
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "warn", Component: "codex", Code: "reconnect_needs_review", Generation: epoch, SessionEpoch: epoch, Phase: "original_cleanup_receipt", Sequence: uint64(attempt)})
		}
		if stop || attempt == maxAutoReconnectAttempts {
			if !ready && !needsReview && s.state.Connection != "login" {
				s.state.Connection = "offline"
				s.state.Message = "自动恢复未能核对原任务；请检查连接后手动重试。"
				if resourceExhausted(s.lastConnectCause) {
					s.state.ConnectionIssue = "resource_exhausted"
					s.state.Message = "本机连接资源暂时不足；原任务仍未确认。请释放资源后手动重试。"
				}
				s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "reconnect_exhausted", Reason: transportCode(s.lastConnectCause), Generation: epoch, SessionEpoch: epoch, Phase: "awaiting_manual_reconnect", Sequence: uint64(attempt)})
				s.update()
			}
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
