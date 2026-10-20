package weixin

import "errors"

// Keep the original durable inbox, cursor, delivery intents and pause setting.
// Close's existing join owns channel effects; no second delivery ledger exists.
func (b *Bridge) PrepareUpdate() error {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	if b.closed || b.loadErr || b.pairCancel != nil || len(b.secretInbox) != 0 {
		b.mu.Unlock()
		return errors.New("channel interaction pending")
	}
	b.updating = true
	b.mu.Unlock()
	b.stopLocked()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.secretInbox) != 0 {
		return errors.New("channel interaction pending")
	}
	return b.saveLocked()
}

func (b *Bridge) ResumeAfterUpdate() {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	b.mu.Lock()
	resume := b.updating && !b.closed
	b.updating = false
	b.mu.Unlock()
	if resume {
		b.startLocked()
	}
}

func (b *Bridge) CloseForUpdate() error {
	b.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saveLocked()
}
