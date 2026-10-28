package desktop

import (
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var errShortcutConflict = errors.New("shortcut conflict")

type shortcutRegistrationError struct{ Code int }

func (e *shortcutRegistrationError) Error() string {
	return fmt.Sprintf("native shortcut registration: %d", e.Code)
}
func (e *shortcutRegistrationError) Unwrap() error {
	if e.Code == -9878 {
		return errShortcutConflict
	} // Carbon eventHotKeyExistsErr
	return nil
}

type shortcutRecoveryDriver interface{ shortcutRecoveryInterval() time.Duration }
type shortcutRetry struct {
	next     time.Time
	failures uint8
}

func (s *Service) shortcutStates() [4]*ShortcutState {
	return [4]*ShortcutState{&s.shortcut, &s.taskShortcut, &s.capture.shortcuts[0], &s.capture.shortcuts[1]}
}
func (s *Service) effectiveShortcut(v Shortcut, slot int) Shortcut {
	if slot >= 2 {
		v.Enabled = v.Enabled && s.capture.preferences.Enabled
	}
	return v
}
func (s *Service) registerShortcutSlot(v Shortcut, slot int) error {
	switch slot {
	case 0:
		if d, ok := s.native.(shortcutDriver); ok {
			return d.registerShortcut(v)
		}
	case 1:
		if d, ok := s.native.(taskShortcutDriver); ok {
			return d.registerTaskShortcut(v)
		}
	default:
		if d, ok := s.native.(captureDriver); ok {
			return d.registerCaptureShortcut(v, slot-2)
		}
	}
	return errors.New("native shortcut driver unavailable")
}
func (s *Service) shortcutResult(slot int, enabled bool, err error) {
	state := s.shortcutStates()[slot]
	previous := state.Status
	configurationWarning := state.Message
	state.Registered = enabled && err == nil
	state.Status = "disabled"
	state.Message = ""
	if previous == "" && err == nil {
		state.Message = configurationWarning
	}
	if enabled {
		switch {
		case err == nil:
			state.Status = "available"
		case errors.Is(err, errShortcutConflict):
			state.Status = "conflict"
		default:
			state.Status = "unavailable"
		}
	}
	if err != nil {
		state.Message = s.text("native.shortcutUnavailable", nil)
	}
	if previous != state.Status {
		attrs := []any{"slot", slot, "state", state.Status}
		var native *shortcutRegistrationError
		if errors.As(err, &native) {
			attrs = append(attrs, "native_status", native.Code)
		}
		slog.Info("Desktop shortcut state changed", attrs...)
	}
	if err == nil || !enabled {
		s.shortcutRetries[slot] = shortcutRetry{}
	}
}

// The same lock serializes saves, feature-gate changes, recovery and shutdown.
// No queued attempt carries an old key/configuration across a later user edit.
func (s *Service) recoverShortcuts(now time.Time, force bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	for slot, state := range s.shortcutStates() {
		v := s.effectiveShortcut(state.Shortcut, slot)
		retry := &s.shortcutRetries[slot]
		if !v.Enabled || state.Registered {
			continue
		}
		if !force && now.Before(retry.next) {
			continue
		}
		var err error
		if s.captureShortcutConflict(v, slot) {
			err = errShortcutConflict
		} else {
			err = s.registerShortcutSlot(v, slot)
		}
		s.shortcutResult(slot, v.Enabled, err)
		if err != nil {
			if retry.failures < 4 {
				retry.failures++
			}
			retry.next = now.Add(time.Second * time.Duration(1<<(retry.failures-1)))
		}
	}
}
func (s *Service) startShortcutRecovery() {
	d, ok := s.native.(shortcutRecoveryDriver)
	if !ok {
		return
	}
	interval := d.shortcutRecoveryInterval()
	if interval <= 0 {
		return
	}
	s.shortcutRecoveryStop = make(chan struct{})
	s.shortcutRecoveryWake = make(chan struct{}, 1)
	stop, wake := s.shortcutRecoveryStop, s.shortcutRecoveryWake
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				s.recoverShortcuts(now, false)
			case <-wake:
				s.recoverShortcuts(time.Now(), true)
			}
		}
	}()
}
func (s *Service) requestShortcutRecovery() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	select {
	case s.shortcutRecoveryWake <- struct{}{}:
	default:
	}
}

// Save a valid user choice even when the OS reports temporary contention.
// A failed write keeps the previous configuration and native registration.
func (s *Service) saveShortcutSlot(v Shortcut, slot int, file string) (ShortcutState, error) {
	state := s.shortcutStates()[slot]
	old := *state
	if s.stopped {
		return old, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	active := s.effectiveShortcut(v, slot)
	if s.captureShortcutConflict(active, slot) {
		return old, errors.New(s.text("native.shortcutConflict", nil))
	}
	err := s.registerShortcutSlot(active, slot)
	if err != nil && !errors.Is(err, errShortcutConflict) {
		return old, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	if saveErr := saveShortcut(file, v); saveErr != nil {
		if err == nil {
			previous := s.effectiveShortcut(old.Shortcut, slot)
			previous.Enabled = old.Registered
			if rollback := s.registerShortcutSlot(previous, slot); rollback != nil {
				previous.Enabled = false
				_ = s.registerShortcutSlot(previous, slot)
				s.shortcutResult(slot, s.effectiveShortcut(old.Shortcut, slot).Enabled, rollback)
			}
		}
		return *state, errors.New(s.text("native.shortcutSaveFailed", nil))
	}
	if err != nil {
		// A failed replacement leaves the old native registration intact. Retire it
		// after persistence, so a saved conflicting new key cannot keep the old key live.
		disabled := active
		disabled.Enabled = false
		if stopErr := s.registerShortcutSlot(disabled, slot); stopErr != nil {
			// Keep the saved desired key; never report the old registration as its success.
			err = stopErr
		}
	}
	state.Shortcut = v
	s.shortcutRetries[slot] = shortcutRetry{}
	s.shortcutResult(slot, active.Enabled, err)
	return *state, nil
}
