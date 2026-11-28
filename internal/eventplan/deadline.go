package eventplan

import "time"

// Native readiness and receipt changes wake the host independently of timers.
func (e *Engine) NextCheck(now time.Time) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped || e.writing {
		return time.Time{}
	}
	if e.retry != nil || len(e.results) > 0 {
		return now
	}
	var next time.Time
	offer := func(at time.Time) {
		if !at.IsZero() && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	for _, o := range e.state.Occurrences {
		if o.Status == "pending" {
			offer(now)
		}
		if !o.Finished.IsZero() {
			offer(o.Finished.Add(Retention))
		}
	}
	for _, p := range e.state.Plans {
		if e.pending(p.ID) {
			continue
		}
		if !p.Finished.IsZero() {
			if e.grantRetirable(p) {
				offer(e.lastRevoke.Add(5 * time.Second))
			} else if retainFrom := e.planRetentionStart(p); !retainFrom.IsZero() {
				offer(retainFrom.Add(Retention))
			}
			continue
		}
		offer(p.Expires)
		v, _ := e.event(p.Event)
		if p.Status == "active" && (v.Kind == "wake" || e.running == "" && !e.observationUnresolved(v.ID)) {
			offer(p.Next)
		}
	}
	return next
}
