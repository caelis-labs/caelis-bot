package codex

import (
	"context"
	"errors"
)

func (s *Session) DiagnosticStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	transport := "unavailable"
	if s.client != nil {
		err := s.client.Err()
		switch {
		case err == nil:
			transport = "connected"
		case errors.Is(err, ErrEventOverflow):
			transport = "event_overflow"
		case errors.Is(err, ErrProtocol):
			transport = "protocol"
		case errors.Is(err, context.DeadlineExceeded):
			transport = "timeout"
		case errors.Is(err, ErrClosed):
			transport = "closed"
		default:
			transport = "disconnected"
		}
	}
	owned := s.ownsLiveRuntimeLocked()
	terminalAvailable := false
	if s.liveRuntimeLocked() {
		endpoint, ok := s.client.rpc.conn.(interface{ terminalEndpoint() string })
		terminalAvailable = ok && endpoint.terminalEndpoint() != ""
	}
	return map[string]any{"transport": transport, "bound": s.bound, "pendingSubmission": s.binding.Pending != nil, "errorLog": s.opts.Diagnostics.Status(),
		"ownedWorkers": len(s.children), "activeWorkers": len(s.childRuns), "pagedHistory": s.historyPaged, "schemaBaseline": TestedVersion,
		"ownsLiveRuntime": owned, "fenceable": owned && OwnedRuntimeSupported(), "terminalEndpointAvailable": terminalAvailable}
}
