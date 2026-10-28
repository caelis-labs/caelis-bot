package machines

import "github.com/caelis-labs/caelis-bot/internal/diagnosticlog"

// The App supplies its bounded nonblocking sink, shared with native runtimes.
func (s *Service) SetDiagnostics(emit func(diagnosticlog.Record)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diagnostics = emit
	s.ssh.SetDiagnostics(emit)
}

func (s *Service) diagnoseHealthLocked(before, after profile) {
	if s.diagnostics == nil || (before.View.State == after.View.State && before.View.Issue == after.View.Issue && before.View.PairingStatus == after.View.PairingStatus) {
		return
	}
	// Only locally assigned states, never remote errors or receipt payloads.
	phase := "peer_unavailable"
	switch after.View.State {
	case "ready":
		phase = "ready"
	case "setup":
		phase = "runtime_unavailable"
	case "authorization":
		phase = "authorization_required"
	}
	s.diagnostics(diagnosticlog.Record{Level: "info", Component: "machines", Code: "peer_health_changed", Phase: phase,
		Item: diagnosticlog.Fingerprint([]byte(after.View.ID)), Fingerprint: diagnosticlog.Fingerprint([]byte(after.Peer.Address))})
}
