package telegram

import (
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// PrepareUpdate uses the existing poll/dispatch shutdown join, without changing
// Enabled, pairing, the receive queue or any original delivery receipt.
func (b *Bridge) PrepareUpdate() error {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	if b.closed || b.loadErr != nil || len(b.secretIngress) != 0 || (b.nonce != "" && time.Now().Before(b.expires)) {
		b.mu.Unlock()
		return errors.New("channel interaction pending")
	}
	b.updating = true
	b.mu.Unlock()
	b.stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.secretIngress) != 0 {
		return errors.New("channel interaction pending")
	}
	// Also joins an earlier timed-out localstate.Write before publishing the
	// final checkpoint. A historical unknown is retained, never replayed here.
	return localstate.WriteConfirmed(b.path, b.state)
}

func (b *Bridge) ResumeAfterUpdate() {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	resume := b.updating && b.state.Enabled && !b.closed
	b.updating = false
	b.mu.Unlock()
	if resume {
		b.resume()
	}
}

func (b *Bridge) CloseForUpdate() error {
	b.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.loadErr != nil {
		return b.loadErr
	}
	return localstate.WriteConfirmed(b.path, b.state)
}
