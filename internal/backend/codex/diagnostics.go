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
		case errors.Is(err, ErrJSONDecode):
			transport = "json_decode_failed"
		case errors.Is(err, ErrDuplicateRequest):
			transport = "duplicate_server_request"
		case errors.Is(err, ErrProtocol):
			transport = "protocol"
		case errors.Is(err, ErrFrameTooLarge):
			transport = "frame_too_large"
		case errors.Is(err, ErrWebSocketReset):
			transport = "websocket_reset"
		case errors.Is(err, ErrWebSocketClose):
			transport = "websocket_close"
		case errors.Is(err, ErrIO):
			transport = "io_failure"
		case errors.Is(err, context.DeadlineExceeded):
			transport = "timeout"
		case errors.Is(err, ErrClosed):
			transport = "closed"
		default:
			transport = "disconnected"
		}
	}
	return map[string]any{"transport": transport, "bound": s.bound, "pendingSubmission": s.binding.Pending != nil, "errorLog": s.opts.Diagnostics.Status(),
		"ownedWorkers": len(s.children), "activeWorkers": len(s.childRuns), "pagedHistory": s.historyPaged, "schemaBaseline": TestedVersion}
}
