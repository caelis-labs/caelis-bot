package codex

import (
	"context"
	"errors"
	"os"
	"strings"
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
	if attempt > maxAutoReconnectAttempts {
		return min(5*time.Minute, 30*time.Second<<min(attempt-maxAutoReconnectAttempts-1, 4))
	}
	return (250 * time.Millisecond) << min(attempt-1, 4)
}

// A saved public endpoint can be probed indefinitely without changing the
// original owner. Private owners and retained Workers keep their own fences.
func (s *Session) supervisesSharedLocked() bool {
	return s.epoch > 0 && !s.workerOnly && s.loadErr == nil && s.retainedOwner == nil &&
		s.binding.OwnerProcess == nil && !s.binding.OwnerUnavailable && !s.binding.OwnerClosing &&
		sharedHome(strings.TrimPrefix(s.binding.OwnerEndpoint, "unix://")) != ""
}

func (s *Session) retryableSharedFailureLocked() bool {
	err := s.lastConnectCause
	if s.state.Connection != "offline" || s.trustBlockedReason != "" || err == nil ||
		errors.Is(err, errRuntimeMissing) || incompatibleProtocol(err) || resourceExhausted(err) || errors.Is(err, os.ErrPermission) {
		return false
	}
	// A foreground caller can expire or cancel during initialize while the
	// resident is still meant to be online.
	return errors.Is(err, errExistingServer) || recoverableDisconnect(err) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) && s.life.Err() == nil
}

// Only the current episode can receive a wake edge. Signals received during
// a probe are absorbed; they must not bypass the next backoff.
func (s *Session) RequestReconnectProbe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reconnectActive && s.reconnectWake != nil {
		select {
		case s.reconnectWake <- struct{}{}:
			resetSharedCooldown(strings.TrimPrefix(s.binding.OwnerEndpoint, "unix://"))
		default:
		}
	}
}

// Called under s.mu after the original listener has stopped. The one coordinator
// owns all retries; listeners created by its attempts cannot start a second one.
func (s *Session) scheduleAutoReconnectLocked(c *Client, epoch uint64) {
	if (!s.bound && !s.workerOnly && !s.supervisesSharedLocked()) || !recoverableDisconnect(c.Err()) {
		return
	}
	s.startAutoReconnectLocked(epoch)
}

func (s *Session) cancelAutoReconnectLocked() {
	s.reconnectSeq++
	if s.reconnectCancel != nil {
		s.reconnectCancel()
	}
	s.reconnectActive = false
	s.reconnectWake = nil
	s.reconnectCancel = nil
}

func (s *Session) startAutoReconnectLocked(epoch uint64) {
	if s.reconnectActive || s.closed || s.closing {
		return
	}
	ctx, cancel := context.WithCancel(s.life)
	s.reconnectSeq++
	seq := s.reconnectSeq
	s.reconnectActive = true
	s.reconnectCancel = cancel
	s.reconnectAttempts = 0
	s.reconnectNoticeID = s.run
	if s.binding.Pending != nil {
		s.reconnectNoticeID = s.binding.Pending.ID
	}
	if s.reconnectNoticeID == "" && s.state.Phase == "unknown" && s.binding.LastReceipt != nil {
		s.reconnectNoticeID = s.binding.LastReceipt.ID
	}
	s.state.Message = "连接中断，正在核对原任务和发送结果。"
	shared := s.supervisesSharedLocked()
	go func() {
		defer cancel()
		s.autoReconnect(ctx, seq, epoch, shared)
	}()
}

func (s *Session) autoReconnect(ctx context.Context, seq, epoch uint64, shared bool) {
	defer func() {
		s.mu.Lock()
		if s.reconnectSeq == seq {
			s.reconnectActive = false
			s.reconnectCancel = nil
			s.reconnectWake = nil
			// The recovered transport can close just after connect returns and
			// before this coordinator releases the episode.
			if shared && ctx.Err() == nil && s.supervisesSharedLocked() && s.retryableSharedFailureLocked() {
				s.startAutoReconnectLocked(s.epoch)
			}
			s.update()
		}
		s.mu.Unlock()
	}()
	for attempt := 1; ; attempt++ {
		var wake chan struct{}
		if shared && attempt > maxAutoReconnectAttempts {
			s.mu.Lock()
			if s.reconnectSeq != seq || ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			wake = make(chan struct{}, 1)
			s.reconnectWake = wake
			s.update()
			s.mu.Unlock()
		}
		timer := time.NewTimer(s.retryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
		s.op.Lock()
		s.mu.Lock()
		current := s.reconnectSeq == seq && s.epoch == epoch && !s.closed && !s.closing && ctx.Err() == nil
		if current {
			s.reconnectAttempts = attempt
			s.reconnectWake = nil
			phase, limit := "original_owner", maxAutoReconnectAttempts
			if attempt > maxAutoReconnectAttempts {
				phase, limit = "background_probe", 0
			}
			s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "reconnect_attempt", Generation: epoch, SessionEpoch: epoch, Phase: phase, Limit: limit, Sequence: uint64(attempt)})
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
		if shared {
			stop = !s.supervisesSharedLocked() || !s.retryableSharedFailureLocked()
		}
		if stop || !shared && attempt == maxAutoReconnectAttempts {
			if !ready && !needsReview && s.state.Connection != "login" {
				s.state.Connection = "offline"
				if !shared && !errors.Is(s.lastConnectCause, errSharedStartFailed) {
					s.state.Message = "自动恢复未能核对原任务；请检查连接后手动重试。"
				}
				if resourceExhausted(s.lastConnectCause) {
					s.state.ConnectionIssue = "resource_exhausted"
					s.state.Message = "本机连接资源暂时不足；原任务仍未确认。请释放资源后手动重试。"
				}
				s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "reconnect_exhausted", Reason: transportCode(s.lastConnectCause), Generation: epoch, SessionEpoch: epoch, Phase: "awaiting_manual_reconnect", Sequence: uint64(attempt)})
				// Task alerts use their own execution ledger. A resident request
				// also needs its original-request reminder when tasks are active.
				if identity := s.reconnectNoticeID; identity != "" && !s.workerOnly {
					key := opaque(s.binding.ThreadID, identity)
					if s.binding.RecoveryNotices == nil {
						s.binding.RecoveryNotices = map[string]bool{}
					}
					if !s.binding.RecoveryNotices[key] {
						s.binding.RecoveryNotices[key] = true
						if s.save() == nil {
							s.state.RecoveryNoticeKey = key
						} else {
							delete(s.binding.RecoveryNotices, key)
						}
					}
				}
				s.update()
			}
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
}
