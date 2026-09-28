package care

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type Policy struct {
	MaximumInterruptionsPer24Hours int `json:"maximumInterruptionsPer24Hours"`
	MinimumGapSeconds              int `json:"minimumGapSeconds"`
}

func DefaultPolicy() Policy { return Policy{8, 300} }
func (p Policy) valid() bool {
	return p.MaximumInterruptionsPer24Hours >= 1 && p.MaximumInterruptionsPer24Hours <= 256 && p.MinimumGapSeconds >= 0 && p.MinimumGapSeconds <= 86400
}

// Configure changes only admission policy, never rule or runtime authority.
func (e *Engine) Configure(ctx context.Context, p Policy, authorize func(context.Context, string, string) error) error {
	if !p.valid() {
		return errors.New("care policy requires 1–256 interruptions per day and a 0–86400 second dispatch gap")
	}
	e.op.Lock()
	defer e.op.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.mu.Lock()
	if e.fatal != nil {
		err := e.fatal
		e.mu.Unlock()
		return err
	}
	if e.state.Policy == p {
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()
	if authorize != nil {
		raw, _ := json.Marshal(p)
		if err := authorize(ctx, "care-policy:limits", string(raw)); err != nil {
			return err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	next := copyState(e.state)
	next.Policy = p
	return e.commit(next)
}

type Budget struct {
	InterruptionsUsed     int `json:"interruptionsUsed"`
	Reserved              int `json:"reserved"`
	MigrationReservations int `json:"migrationReservations"`
	Remaining             int `json:"remaining"`
}

func budget(s State, now time.Time) Budget {
	b := Budget{}
	for _, at := range s.Interruptions {
		if at.After(now.Add(-24 * time.Hour)) {
			b.InterruptionsUsed++
		}
	}
	for _, at := range s.LegacyAttempts {
		if at.After(now.Add(-24 * time.Hour)) {
			b.MigrationReservations++
		}
	}
	for _, a := range s.Activations {
		if inFlight(a) && a.VisibleAt.IsZero() {
			b.Reserved++
		}
	}
	b.Remaining = max(0, s.Policy.MaximumInterruptionsPer24Hours-b.InterruptionsUsed-b.Reserved-b.MigrationReservations)
	return b
}
func (e *Engine) Budget(now time.Time) Budget {
	e.mu.Lock()
	defer e.mu.Unlock()
	return budget(e.state, now)
}
