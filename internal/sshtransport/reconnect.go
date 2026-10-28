package sshtransport

import (
	"math/rand/v2"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

const maxRetryDelay = 2 * time.Minute

// Health reads drive recovery; there is no second supervisor or pairing loop.
// Only classified transient transport failures permit noninteractive recovery.
func retryable(issue string) bool {
	switch issue {
	case "ssh_connection_failed", "ssh_disconnected", "ssh_host_unreachable", "ssh_host_not_found", "ssh_forward_failed":
		return true
	}
	return false
}

func retryDelay(failures int) time.Duration {
	base := min(5*time.Second<<min(max(failures-1, 0), 5), maxRetryDelay)
	// 80–100% jitter, including at the cap. No accumulated/lifetime budget.
	return base - time.Duration(rand.Int64N(int64(base/5)+1))
}

// The app supplies its existing bounded, nonblocking diagnostic sink. No raw
// stderr, host, username, address, config, prompt or credential leaves this layer.
func (t *Transport) SetDiagnostics(emit func(diagnosticlog.Record)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.diagnostics = emit
}

func (t *Transport) diagnoseLocked(c *tunnel, phase, issue string, delay time.Duration, exit *int) {
	if t.diagnostics == nil {
		return
	}
	key, _ := targetKey(c.target)
	t.diagnostics(diagnosticlog.Record{
		Level: "info", Component: "sshtransport", Code: "ssh_channel_lifecycle",
		Fingerprint: diagnosticlog.Fingerprint([]byte(key)), Generation: c.generation,
		Phase: phase, Reason: issue, ProcessID: c.pid, ExitCode: exit,
		ConsecutiveFailures: c.failures, RetryAfterMS: delay.Milliseconds(),
	})
}
