package eventplan

import (
	"slices"
	"time"
)

// NextCheck returns the earliest clock-driven work. Changes and collector
// completion have their own wake edge. Native receipt changes wake the host.
func (e *Engine) NextCheck(now time.Time) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped || e.fault != nil {
		return time.Time{}
	}
	if len(e.results) > 0 {
		return now
	}
	var next time.Time
	offer := func(at time.Time) {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	for _, at := range e.state.WakeAttempts {
		offer(at.Add(24 * time.Hour))
	}
	for _, at := range e.state.WakeReservations {
		offer(at.Add(24 * time.Hour))
	}
	budget := len(e.state.WakeAttempts)+len(e.state.WakeReservations) < e.state.Policy.MaximumInterruptionsPer24Hours
	wakeAt := now
	if n := len(e.state.WakeAttempts); n > 0 {
		wakeAt = later(wakeAt, e.state.WakeAttempts[n-1].Add(time.Duration(e.state.Policy.MinimumGapSeconds)*time.Second))
	}
	for _, o := range e.state.Occurrences {
		switch {
		case o.Status == "pending":
			offer(o.expiry())
			if budget && !e.wakeBlocked(o) {
				offer(wakeAt)
			}
		case !o.Finished.IsZero() && !slices.ContainsFunc(e.state.Occurrences, func(other Occurrence) bool { return other.AttentionFor == o.ID }):
			offer(o.Finished.Add(Retention + time.Nanosecond))
		}
	}
	revokeAt := later(now, e.lastRevoke.Add(5*time.Second))
	for _, p := range e.state.Plans {
		if e.pending(p.ID) {
			continue
		}
		if !p.Finished.IsZero() {
			if !p.Retired {
				offer(revokeAt)
			} else if !slices.ContainsFunc(e.state.Occurrences, func(o Occurrence) bool { return o.Plan == p.ID }) {
				offer(p.Finished.Add(Retention + time.Nanosecond))
			}
			continue
		}
		offer(p.Expires)
		if p.Status == "active" && p.Kind != "native" && e.running == "" && !e.unknownEvent(p.Event) {
			offer(later(later(p.Next, e.lastStart.Add(5*time.Second)), p.LastWake.Add(time.Duration(p.CooldownSeconds)*time.Second)))
		}
	}
	for _, v := range e.state.Events {
		switch {
		case v.Archived.IsZero():
			if !slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Event == v.ID && (p.Finished.IsZero() || e.pending(p.ID)) }) {
				offer(v.LastUsed.Add(24 * time.Hour))
			}
		case !v.Retired:
			offer(revokeAt)
		case !slices.ContainsFunc(e.state.Plans, func(p Plan) bool { return p.Event == v.ID }):
			offer(v.Archived.Add(Retention + time.Nanosecond))
		}
	}
	return next
}

func later(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func (o Occurrence) expiry() time.Time {
	if o.Expires.IsZero() {
		return o.At.Add(time.Hour)
	}
	return o.Expires
}

func (e *Engine) wakeBlocked(o Occurrence) bool {
	return slices.ContainsFunc(e.state.Occurrences, func(other Occurrence) bool {
		return other.Plan == o.Plan && other.ID != o.ID && (other.Status == "unknown_wake" || other.Status == "dispatching" || other.Status == "accepted")
	})
}
