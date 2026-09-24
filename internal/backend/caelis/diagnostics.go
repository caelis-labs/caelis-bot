package caelis

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func (s *Session) DiagnosticStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{"connected": s.connected, "errorLog": s.diagnostics.Status()}
}

func (s *Session) logEnvelope(e wire.Envelope) {
	if e.Kind == "caelis/error" {
		message := value(e.Error)
		s.diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "caelis", Code: "runtime_error", Method: e.Kind,
			Thread: value(e.SessionId), Turn: value(e.TurnId), Item: value(e.EventId), Reason: diagnosticlog.Reason(message), Fingerprint: diagnosticlog.Fingerprint([]byte(message))})
	}
}
