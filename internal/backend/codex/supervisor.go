package codex

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

// Fast retries are bounded; a quiet supervisor keeps recoverable features from
// remaining unavailable forever. It observes/reconnects the original owner only.
func (s *Session) supervise() {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	var nextConnect time.Time
	for {
		select {
		case <-s.life.Done():
			return
		case <-timer.C:
		}
		if !s.mu.TryLock() {
			continue
		}
		if s.closed || s.closing {
			s.mu.Unlock()
			return
		}
		c, epoch := s.client, s.epoch
		reconnect := (epoch > 0 || s.loadErr != nil) && s.state.Connection == "offline" && !s.reconnectActive && time.Now().After(nextConnect)
		ready := s.state.Connection == "ready" && c != nil && c.Err() == nil
		syncRecent := ready && s.residentSyncNeeded && !s.workerOnly
		thread, revision := s.binding.ThreadID, s.state.Revision
		if ready {
			s.recoverUnresolvedChildren(c, epoch)
		}
		s.mu.Unlock()
		if reconnect {
			nextConnect = time.Now().Add(30 * time.Second)
			ctx, cancel := context.WithTimeout(s.life, 30*time.Second)
			_ = s.Connect(ctx)
			cancel()
		}
		if syncRecent {
			ctx, cancel := context.WithTimeout(s.life, 5*time.Second)
			native, err := readThreadState(ctx, c, thread)
			cancel()
			if !s.mu.TryLock() {
				continue
			}
			if err == nil && s.client == c && s.epoch == epoch {
				if s.state.Revision == revision {
					for _, turn := range native.Turns {
						s.applyTurn(turn, true)
					}
					if s.binding.Pending == nil && len(s.binding.CleanupTargets) == 0 && len(native.Turns) > 0 {
						latest := native.Turns[len(native.Turns)-1]
						if terminal(latest.Status) && s.run == "" && !s.hasConversationPrompt() {
							s.state.Phase = latest.Status
						}
					}
				}
				s.residentSyncNeeded = false
				if s.state.Message == "最近消息暂时未同步；连接可用，将自动重试" {
					s.state.Message = ""
				}
				s.update()
			}
			s.mu.Unlock()
		}
	}
}

// A repaired original record can be adopted. An absent or still-invalid record
// is never replaced by a new conversation after a previous read failure.
func (s *Session) reloadBindingLocked() {
	if validateExecution(s.opts.Execution) != nil {
		return
	}
	data, err := os.ReadFile(s.opts.StateFile)
	var restored binding
	if err != nil || json.Unmarshal(data, &restored) != nil || restored.Version != 1 {
		return
	}
	if restored.Scheduled == nil {
		restored.Scheduled = map[string]string{}
	}
	if restored.HostReports == nil {
		restored.HostReports = map[string]bool{}
	}
	s.binding, s.loadErr = restored, nil
	s.state.Message = ""
}
