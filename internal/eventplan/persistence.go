package eventplan

import (
	"errors"
	"maps"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// ErrWritePending means a previous write or its recovery checkpoint still owns
// the ledger. Reads can continue against the last confirmed state.
var ErrWritePending = errors.New("event storage write is settling; preserve original receipts")

type writeKind uint8

const (
	writeChange writeKind = iota
	writeIntent
	writeObservation
)

// Entries' nested values are immutable once admitted. The scheduler modifies
// only these containers and replaces, rather than edits, their nested values.
func copyState(s State) State {
	s.Events = slices.Clone(s.Events)
	s.Plans = slices.Clone(s.Plans)
	s.Occurrences = slices.Clone(s.Occurrences)
	s.WakeAttempts = slices.Clone(s.WakeAttempts)
	s.WakeReservations = slices.Clone(s.WakeReservations)
	s.NativeWatermarks = maps.Clone(s.NativeWatermarks)
	return s
}

func (e *Engine) persist() error { return e.persistAs(writeChange) }

func (e *Engine) persistAs(kind writeKind) error {
	next := e.state
	err := e.write(e.path, next)
	var failure *localstate.WriteError
	if !errors.As(err, &failure) || failure.Completion == nil {
		e.rememberFailure(err)
		e.settleWrite(next, kind, err, false)
		return err
	}

	// Only the engine consumes the writer's terminal result. Diagnostics get a
	// copy so they cannot race the ordered publication of the next state.
	var diagnostic chan localstate.WriteCompletion
	if e.firstFailure == nil {
		diagnostic = make(chan localstate.WriteCompletion, 1)
		first := *failure
		first.Completion = diagnostic
		e.rememberFailure(&first)
	}
	e.state = copyState(e.saved)
	e.writing = true
	go func() {
		result := <-failure.Completion
		e.mu.Lock()
		e.settleWrite(next, kind, result.Err, true)
		e.writing = false
		e.mu.Unlock()
		if diagnostic != nil {
			diagnostic <- result
		}
		e.signal()
	}()
	wait := *failure
	wait.Completion = nil
	return errors.Join(ErrWritePending, &wait)
}

func (e *Engine) rememberFailure(err error) {
	if err != nil && e.firstFailure == nil {
		e.firstFailure = err
		e.signal()
	}
}

func (e *Engine) settleWrite(next State, kind writeKind, err error, late bool) {
	if err == nil && !(late && kind == writeIntent) {
		e.saved = copyState(next)
		e.state = next
		return
	}
	e.state = copyState(e.saved)
	// A parent sync error means rename already installed the candidate. Seal
	// the chosen state before allowing another operation to build on it.
	installed := err == nil || localstate.DescribeFailure(err).Stage == "parent_sync"
	switch {
	case kind == writeIntent && installed:
		// The caller returned without invoking the effect. Remove its unused
		// fence durably before a fresh admission can consider dispatch.
		prior := copyState(e.saved)
		e.retry = &prior
	case kind == writeObservation || installed:
		// Observations may describe effects already invoked. A retry saves the
		// exact result, never re-runs the effect or consumes a new identity.
		e.retry = &next
	}
}

func (e *Engine) retryWrite() error {
	if e.retry == nil {
		return nil
	}
	e.state = *e.retry
	e.retry = nil
	return e.persistAs(writeObservation)
}
